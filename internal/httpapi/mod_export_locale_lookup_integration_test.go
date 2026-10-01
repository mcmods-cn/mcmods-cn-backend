package httpapi

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExportLocaleLookupDoesNotRelabelRegionalTranslationsIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to an isolated migrated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
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
	unique := "apia-locale-" + randomHex(8)
	var modID, versionID, fileID int64
	if err = tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values(new_public_id(),$1,'Locale fixture','approved') returning id`, unique).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status)
		values($1,'Locale fixture',array['1.20.1'],array['forge'],'active') returning id`, modID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_packages(id,sha256,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest)
		values($1,$2,'fixture.zip','mcmods-export/v1','0.6.0','1.20.1','forge','{}')`, unique, randomHex(32)); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version,status)
		values($1,$2,$1,$3,$4,'ready')`, unique, modID, versionID, modExportImporterVersion); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_revisions(id,mod_id,package_id,job_id,target_version_id,revision_no,status,minecraft_version,loader,exporter_version,source_namespace)
		values($1,$2,$1,$1,$3,1,'ready','1.20.1','forge','0.6.0','minecraft')`, unique, modID, versionID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into oss_files(bucket,object_key,content_type,status,scan_status)
		values('synthetic',$1,'application/gzip','active','clean') returning id`, unique+"/zh-TW.json.gz").Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_locales(revision_id,locale,translation_count) values($1,'zh-TW',1)`, unique); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_media(revision_id,asset_path,media_kind,oss_file_id,sha256,content_type,byte_length)
		values($1,'_locales/zh-TW.json.gz',$2,$3,$4,'application/gzip',128)`, unique, exportLocaleBundleKind, fileID, randomHex(32)); err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"zh-CN", "zh-HK", "zh-MO", "zh-Hans", "zh"} {
		var bucket, key string
		err = tx.QueryRow(ctx, exportLocaleBundleLookupSQL, unique, locale, exportLocaleBundleKind).Scan(&bucket, &key)
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("locale %s selected Taiwan's bundle: key=%q error=%v", locale, key, err)
		}
	}
	var bucket, key string
	if err = tx.QueryRow(ctx, exportLocaleBundleLookupSQL, unique, "zh-TW", exportLocaleBundleKind).Scan(&bucket, &key); err != nil {
		t.Fatal(err)
	}
	if bucket != "synthetic" || key != unique+"/zh-TW.json.gz" {
		t.Fatalf("exact locale selected %q/%q", bucket, key)
	}
	// The full real schema and lookup SQL are exercised; OSS HTTP is deliberately
	// not invoked because this regression concerns locale selection/provenance.
}
