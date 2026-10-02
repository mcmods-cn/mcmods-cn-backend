package httpapi

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestImportedItemBlockCategoriesUseSharedAuthorityIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify shared item/block category initialization")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())

	nonce := time.Now().UnixNano()
	var actorID, modID, versionID int64
	if err = tx.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test-only',true) returning id`,
		fmt.Sprintf("system_categories_%d", nonce), fmt.Sprintf("system-categories-%d@example.invalid", nonce)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('reuse0002',$1,'Shared category authority','approved',$2) returning id`,
		fmt.Sprintf("shared-category-%d", nonce), actorID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,status,created_by,updated_by)
		values($1,'Shared category authority','active',$2,$2) returning id`, modID, actorID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}

	packageID, jobID, revisionID := newExportID(), newExportID(), newExportID()
	archiveHash := fmt.Sprintf("%064x", nonce)
	var archiveFileID int64
	if err = tx.QueryRow(ctx, `insert into oss_files(object_key,original_name,sha256,uploader_id)
		values($1,'reuse.zip',$2,$3) returning id`, "tests/reuse-002/"+packageID, archiveHash, actorID).Scan(&archiveFileID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_packages(
		id,sha256,archive_file_id,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest,uploaded_by)
		values($1,$2,$3,'reuse.zip','mcmods-export/v1','test','1.21.1','neoforge','{}',$4)`,
		packageID, archiveHash, archiveFileID, actorID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_jobs(
		id,mod_id,package_id,target_version_id,importer_version,status,created_by)
		values($1,$2,$3,$4,$5,'ready',$6)`,
		jobID, modID, packageID, versionID, modExportImporterVersion, actorID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_revisions(
		id,mod_id,package_id,job_id,target_version_id,revision_no,status,minecraft_version,loader,
		exporter_version,source_namespace,is_active)
		values($1,$2,$3,$4,$5,1,'ready','1.21.1','neoforge','test','minecraft',true)`,
		revisionID, modID, packageID, jobID, versionID); err != nil {
		t.Fatal(err)
	}

	var resourceID int64
	if err = tx.QueryRow(ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status)
		values($1,$2,'resource','active') returning id`,
		fmt.Sprintf("resource:reuse-002:%d", nonce), randomCatalogPublicID()).Scan(&resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into game_resources(
		entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved)
		values($1,'minecraft.block',$2,'minecraft','stone',$3,true)`,
		resourceID, fmt.Sprintf("minecraft:reuse_stone_%d", nonce), modID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into resource_import_snapshots(id,resource_id,revision_id,registry,names,data)
		values($1,$2,$3,'blocks','{"en-US":"Stone"}'::jsonb,'{}'::jsonb)`, newExportID(), resourceID, revisionID); err != nil {
		t.Fatal(err)
	}

	var unrelatedTemplateID, unrelatedRootID, unrelatedChildID int64
	if err = tx.QueryRow(ctx, `select id from mod_content_templates where builtin and code<>'item_block' order by id limit 1`).Scan(&unrelatedTemplateID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into mod_content_sections(
		mod_id,version_id,template_id,default_locale,display_mode,ordinal,status,created_by,updated_by)
		values($1,$2,$3,'en-US','compact',100,'active',$4,$4) returning id`,
		modID, versionID, unrelatedTemplateID, actorID).Scan(&unrelatedRootID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into mod_content_sections(
		mod_id,version_id,template_id,parent_id,system_key,default_locale,display_mode,ordinal,status,created_by,updated_by)
		values($1,$2,$3,$4,'blocks','en-US','compact',0,'active',$5,$5) returning id`,
		modID, versionID, unrelatedTemplateID, unrelatedRootID, actorID).Scan(&unrelatedChildID); err != nil {
		t.Fatal(err)
	}

	if err = ensureImportedContentSectionsTx(ctx, tx, []string{revisionID}, versionID, modID, actorID); err != nil {
		t.Fatal(err)
	}

	var itemBlockRootID int64
	if err = tx.QueryRow(ctx, `select section.id from mod_content_sections section
		join mod_content_templates template on template.id=section.template_id
		where section.version_id=$1 and section.parent_id is null and section.status='active'
		  and template.code='item_block' and template.builtin`, versionID).Scan(&itemBlockRootID); err != nil {
		t.Fatal(err)
	}
	var systemKeys []string
	if err = tx.QueryRow(ctx, `select array_agg(system_key order by ordinal,id)
		from mod_content_sections where version_id=$1 and parent_id=$2 and status='active'`,
		versionID, itemBlockRootID).Scan(&systemKeys); err != nil {
		t.Fatal(err)
	}
	if want := []string{"blocks", "items"}; !reflect.DeepEqual(systemKeys, want) {
		t.Fatalf("system category order=%v want=%v", systemKeys, want)
	}

	var localizedFacts []string
	if err = tx.QueryRow(ctx, `select array_agg(child.system_key||'|'||locale.locale||'|'||locale.name
			order by child.system_key,locale.locale)
		from mod_content_sections child
		join mod_content_section_localizations locale on locale.section_id=child.id
		where child.parent_id=$1 and child.status='active'`, itemBlockRootID).Scan(&localizedFacts); err != nil {
		t.Fatal(err)
	}
	wantLocalizedFacts := []string{
		"blocks|en-US|Blocks", "blocks|zh-CN|方块", "blocks|zh-TW|方塊",
		"items|en-US|Items", "items|zh-CN|物品", "items|zh-TW|物品",
	}
	if !reflect.DeepEqual(localizedFacts, wantLocalizedFacts) {
		t.Fatalf("localized system categories=%v want=%v", localizedFacts, wantLocalizedFacts)
	}
	var unrelatedLocalizationCount int
	if err = tx.QueryRow(ctx, `select count(*)::int from mod_content_section_localizations where section_id=$1`,
		unrelatedChildID).Scan(&unrelatedLocalizationCount); err != nil {
		t.Fatal(err)
	}
	if unrelatedLocalizationCount != 0 {
		t.Fatalf("shared authority localized %d unrelated system-key rows", unrelatedLocalizationCount)
	}
}
