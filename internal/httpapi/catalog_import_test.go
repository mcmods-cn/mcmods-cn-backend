package httpapi

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestPersistCatalogResourcesReusesCanonicalIdentityIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run the catalog import integration test")
		}
		databaseURL = config.Load().DB.ConnString()
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	suffix := time.Now().UnixNano() % 1_000_000
	projectCode := randomCatalogPublicID()
	slug := fmt.Sprintf("catalog-resource-%06d", suffix)
	var modID, versionID int64
	if err = tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values($1,$2,'Catalog resource fixture','approved') returning id`, projectCode, slug).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status)
		values($1,'1.21.1 / NeoForge',array['1.21.1'],array['neoforge'],'active') returning id`, modID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}

	packageID, jobID, revisionID := newExportID(), newExportID(), newExportID()
	archiveHash := fmt.Sprintf("%064x", time.Now().UnixNano())
	if _, err = tx.Exec(ctx, `insert into catalog_import_packages(
		id,sha256,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest)
		values($1,$2,'catalog-resource-test.zip','mcmods-export/v1','test','1.21.1','neoforge','{}')`,
		packageID, archiveHash); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_jobs(
		id,mod_id,package_id,target_version_id,importer_version,status)
		values($1,$2,$3,$4,$5,'ready')`, jobID, modID, packageID, versionID, modExportImporterVersion); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_revisions(
		id,mod_id,package_id,job_id,target_version_id,revision_no,status,minecraft_version,loader,
		exporter_version,source_namespace,is_active)
		values($1,$2,$3,$4,$5,1,'ready','1.21.1','neoforge','test','fixture',true)`,
		revisionID, modID, packageID, jobID, versionID); err != nil {
		t.Fatal(err)
	}

	kindCode := "minecraft.item"
	canonicalID := fmt.Sprintf("fixture:item_%06d", suffix)
	existingIdentity := fmt.Sprintf("resource:existing:%06d", suffix)
	stagedIdentity := fmt.Sprintf("resource:staged:%06d", suffix)
	existingPublicID := randomCatalogPublicID()
	stagedPublicID := randomCatalogPublicID()
	var existingResourceID int64
	if _, err = tx.Exec(ctx, `insert into resource_kinds(code,family,user_visible)
		values($1,'minecraft',true) on conflict(code) do nothing`, kindCode); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status)
		values($1,$2,'resource','active') returning id`, existingIdentity, existingPublicID).Scan(&existingResourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into game_resources(
		entity_id,kind_code,canonical_id,namespace,resource_path,resolved)
		values($1,$2,$3,'fixture',$4,true)`, existingResourceID, kindCode, canonicalID, fmt.Sprintf("item_%06d", suffix)); err != nil {
		t.Fatal(err)
	}

	snapshotID := newExportID()
	rows := []catalogResourceImportRow{
		{
			EntityID: stagedIdentity, PublicID: stagedPublicID, KindCode: kindCode, CanonicalID: canonicalID,
			RawID: "legacy:item", Namespace: "fixture", ResourcePath: fmt.Sprintf("item_%06d", suffix),
			RevisionID: revisionID, SnapshotID: snapshotID, Registry: "items",
			Names: `{"en-US":"Old name","shared":"old"}`, Data: `{"first":1,"shared":"old"}`, IconPath: "old.png",
		},
		{
			EntityID: stagedIdentity, PublicID: stagedPublicID, KindCode: kindCode, CanonicalID: canonicalID,
			RawID: "legacy:item", Namespace: "fixture", ResourcePath: fmt.Sprintf("item_%06d", suffix),
			RevisionID: revisionID, SnapshotID: snapshotID, Registry: "items",
			Names: `{"zh-CN":"新名称","shared":"new"}`, Data: `{"second":2,"shared":"new"}`, IconPath: "new.png",
		},
	}
	if err = persistCatalogResources(ctx, tx, rows); err != nil {
		t.Fatal(err)
	}

	var snapshotResourceID, aliasResourceID int64
	var englishName, chineseName, sharedName, sharedData, iconPath string
	if err = tx.QueryRow(ctx, `select resource_id,names->>'en-US',names->>'zh-CN',names->>'shared',
		data->>'shared',icon_path from resource_import_snapshots
		where revision_id=$1 and resource_id=$2`, revisionID, existingResourceID).
		Scan(&snapshotResourceID, &englishName, &chineseName, &sharedName, &sharedData, &iconPath); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `select resource_id from game_resource_aliases
		where kind_code=$1 and alias_id='legacy:item'`, kindCode).Scan(&aliasResourceID); err != nil {
		t.Fatal(err)
	}
	var stagedIdentityCount int
	if err = tx.QueryRow(ctx, `select count(*)::int from catalog_entities where identity_key=$1`, stagedIdentity).Scan(&stagedIdentityCount); err != nil {
		t.Fatal(err)
	}
	if snapshotResourceID != existingResourceID || aliasResourceID != existingResourceID || stagedIdentityCount != 0 {
		t.Fatalf("canonical identity was not reused: snapshot=%d alias=%d existing=%d staged=%d",
			snapshotResourceID, aliasResourceID, existingResourceID, stagedIdentityCount)
	}
	if englishName != "Old name" || chineseName != "新名称" || sharedName != "new" || sharedData != "new" || iconPath != "new.png" {
		t.Fatalf("snapshot overlays were not merged correctly: en=%q zh=%q name=%q data=%q icon=%q",
			englishName, chineseName, sharedName, sharedData, iconPath)
	}
}
