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
	var uploaderID, modID, versionID int64
	if err = tx.QueryRow(ctx, `insert into users(username,email,password_hash)
		values($1,$2,'test') returning id`, "catalog_import_"+projectCode, "catalog_import_"+projectCode+"@example.invalid").Scan(&uploaderID); err != nil {
		t.Fatal(err)
	}
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
	var archiveFileID int64
	if err = tx.QueryRow(ctx, `insert into oss_files(object_key,original_name,sha256,uploader_id)
		values($1,'catalog-resource-test.zip',$2,$3) returning id`, "tests/catalog-import/"+packageID, archiveHash, uploaderID).Scan(&archiveFileID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_packages(
		id,sha256,archive_file_id,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest,uploaded_by)
		values($1,$2,$3,'catalog-resource-test.zip','mcmods-export/v1','test','1.21.1','neoforge','{}',$4)`,
		packageID, archiveHash, archiveFileID, uploaderID); err != nil {
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
			Names: `{"en-US":"Old name","shared":"old"}`, Data: `{"max_stack_size":16,"enchantment_value":5}`, IconPath: "old.png",
		},
		{
			EntityID: stagedIdentity, PublicID: stagedPublicID, KindCode: kindCode, CanonicalID: canonicalID,
			RawID: "legacy:item", Namespace: "fixture", ResourcePath: fmt.Sprintf("item_%06d", suffix),
			RevisionID: revisionID, SnapshotID: snapshotID, Registry: "items",
			Names: `{"zh-CN":"新名称","shared":"new"}`, Data: `{"max_stack_size":64,"enchantment_value":7}`, IconPath: "new.png",
		},
	}
	if err = persistCatalogResources(ctx, tx, rows); err != nil {
		t.Fatal(err)
	}

	var snapshotResourceID, aliasResourceID int64
	var englishName, chineseName, sharedName, maxStackSize, enchantability, iconPath string
	if err = tx.QueryRow(ctx, `select resource_id,names->>'en-US',names->>'zh-CN',names->>'shared',
		data->>'maxStackSize',data->>'enchantability',icon_path from resource_import_snapshots
		where revision_id=$1 and resource_id=$2`, revisionID, existingResourceID).
		Scan(&snapshotResourceID, &englishName, &chineseName, &sharedName, &maxStackSize, &enchantability, &iconPath); err != nil {
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
	if englishName != "Old name" || chineseName != "新名称" || sharedName != "new" || maxStackSize != "64" || enchantability != "7" || iconPath != "new.png" {
		t.Fatalf("snapshot overlays were not merged correctly: en=%q zh=%q name=%q maxStack=%q enchantability=%q icon=%q",
			englishName, chineseName, sharedName, maxStackSize, enchantability, iconPath)
	}

	archivedCanonicalID := fmt.Sprintf("fixture:archived_%06d", suffix)
	archivedIdentity := resourceIdentity(kindCode, archivedCanonicalID)
	var archivedResourceID int64
	if err = tx.QueryRow(ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status,archived_at)
		values($1,$2,'resource','archived',now()) returning id`, archivedIdentity.ID, archivedIdentity.PublicID).Scan(&archivedResourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,resolved)
		values($1,$2,$3,'fixture',$4,true)`, archivedResourceID, kindCode, archivedCanonicalID, fmt.Sprintf("archived_%06d", suffix)); err != nil {
		t.Fatal(err)
	}
	archivedSnapshotID := catalogSnapshotID("resource", revisionID, archivedIdentity.ID, "")
	if err = persistCatalogResources(ctx, tx, []catalogResourceImportRow{{
		EntityID: archivedIdentity.ID, PublicID: archivedIdentity.PublicID, KindCode: kindCode, CanonicalID: archivedCanonicalID,
		Namespace: "fixture", ResourcePath: fmt.Sprintf("archived_%06d", suffix), RevisionID: revisionID,
		SnapshotID: archivedSnapshotID, Registry: "items", Names: `{"en-US":"Observed again"}`, Data: `{}`,
	}}); err != nil {
		t.Fatal(err)
	}
	var archivedStatus string
	var archivedAtPresent bool
	var archivedObservationCount int
	if err = tx.QueryRow(ctx, `select status,archived_at is not null from catalog_entities where id=$1`, archivedResourceID).
		Scan(&archivedStatus, &archivedAtPresent); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `select count(*)::int from resource_import_snapshots where id=$1 and resource_id=$2`,
		archivedSnapshotID, archivedResourceID).Scan(&archivedObservationCount); err != nil {
		t.Fatal(err)
	}
	if archivedStatus != "archived" || !archivedAtPresent || archivedObservationCount != 1 {
		t.Fatalf("automatic observation changed archive governance: status=%q archivedAt=%v observations=%d",
			archivedStatus, archivedAtPresent, archivedObservationCount)
	}

	unknownCanonicalID := fmt.Sprintf("fixture:shared_%06d", suffix)
	unknownKinds := []string{resourceKindForRegistry("example:widgets"), resourceKindForRegistry("example:gadgets")}
	unknownRows := make([]catalogResourceImportRow, 0, len(unknownKinds))
	for _, unknownKind := range unknownKinds {
		identity := resourceIdentity(unknownKind, unknownCanonicalID)
		unknownRows = append(unknownRows, catalogResourceImportRow{
			EntityID: identity.ID, PublicID: identity.PublicID, KindCode: unknownKind, CanonicalID: unknownCanonicalID,
			Namespace: "fixture", ResourcePath: fmt.Sprintf("shared_%06d", suffix), RevisionID: revisionID,
			SnapshotID: catalogSnapshotID("resource", revisionID, identity.ID, ""), Registry: unknownKind, Names: `{}`, Data: `{}`,
		})
	}
	if err = persistCatalogResources(ctx, tx, unknownRows); err != nil {
		t.Fatal(err)
	}
	var unknownResourceCount, unknownSnapshotCount, documentFamilyCount int
	if err = tx.QueryRow(ctx, `select count(*)::int from game_resources where canonical_id=$1 and kind_code=any($2::text[])`,
		unknownCanonicalID, unknownKinds).Scan(&unknownResourceCount); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `select count(*)::int from resource_import_snapshots snapshot
		join game_resources resource on resource.entity_id=snapshot.resource_id
		where snapshot.revision_id=$1 and resource.canonical_id=$2 and resource.kind_code=any($3::text[])`,
		revisionID, unknownCanonicalID, unknownKinds).Scan(&unknownSnapshotCount); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `select count(*)::int from resource_kinds where code=any($1::text[]) and family='document'`,
		unknownKinds).Scan(&documentFamilyCount); err != nil {
		t.Fatal(err)
	}
	if unknownResourceCount != 2 || unknownSnapshotCount != 2 || documentFamilyCount != 2 {
		t.Fatalf("unknown registries were not isolated: resources=%d snapshots=%d documentFamilies=%d",
			unknownResourceCount, unknownSnapshotCount, documentFamilyCount)
	}

	archivedTagID := fmt.Sprintf("fixture:archived_tag_%06d", suffix)
	archivedTagIdentity := tagIdentity("minecraft:item", archivedTagID)
	var archivedTagEntityID int64
	if err = tx.QueryRow(ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status,archived_at)
		values($1,$2,'tag','archived',now()) returning id`, archivedTagIdentity.ID, archivedTagIdentity.PublicID).Scan(&archivedTagEntityID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into catalog_tags(entity_id,registry,canonical_id)
		values($1,'minecraft:item',$2)`, archivedTagEntityID, archivedTagID); err != nil {
		t.Fatal(err)
	}
	if err = persistExportTags(ctx, tx, nil, []exportTagRow{{
		RevisionID: revisionID, Registry: "minecraft:item", TagID: archivedTagID,
	}}, 0); err != nil {
		t.Fatal(err)
	}
	var archivedTagStatus string
	var archivedTagSnapshotCount int
	if err = tx.QueryRow(ctx, `select status from catalog_entities where id=$1`, archivedTagEntityID).Scan(&archivedTagStatus); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `select count(*)::int from tag_import_snapshots where tag_id=$1 and revision_id=$2`,
		archivedTagEntityID, revisionID).Scan(&archivedTagSnapshotCount); err != nil {
		t.Fatal(err)
	}
	if archivedTagStatus != "archived" || archivedTagSnapshotCount != 1 {
		t.Fatalf("tag observation changed archive governance: status=%q observations=%d", archivedTagStatus, archivedTagSnapshotCount)
	}
}
