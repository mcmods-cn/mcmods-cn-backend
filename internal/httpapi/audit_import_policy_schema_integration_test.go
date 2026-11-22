package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"mcmods-cn-backend/internal/database"
)

func TestAuditImportPolicyInstalledSchemaIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := newTEST018IsolatedDatabase(t, ctx)
	var user, mod, version, file int64
	if err := db.QueryRow(ctx, `insert into users(username,email,password_hash) values('policy-owner','policy@example.invalid','synthetic-only') returning id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,submitted_by) values($1,'policy-project','Policy project',$2) returning id`, randomCatalogPublicID(), user).Scan(&mod); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,status,created_by,updated_by) values($1,'1.20.1','active',$2,$2) returning id`, mod, user).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,sha256,uploader_id) values('fixture','https://example.invalid','fixture','owned-policy-source',repeat('a',64),$1) returning id`, user).Scan(&file); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into catalog_import_packages(id,sha256,archive_file_id,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest,uploaded_by) values('policy-package',repeat('a',64),$1,'synthetic.zip','mcmods-export/v1','test','1.20.1','forge','{}',$2)`, file, user); err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := db.QueryRow(ctx, `insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version,created_by)
		values('policy-job',$1,'policy-package',$2,'fixture/v1',$3) returning requested_publication_mode`, mod, version, user).Scan(&mode); err != nil || mode != "review" {
		t.Fatalf("default mode=%s err=%v", mode, err)
	}
	for _, invalid := range []any{"legacy_unknown", "unexpected", nil} {
		var pgerr *pgconn.PgError
		_, err := db.Exec(ctx, `update catalog_import_jobs set requested_publication_mode=$1 where id='policy-job'`, invalid)
		if !errors.As(err, &pgerr) {
			t.Fatalf("invalid mode %v returned unrelated error: %v", invalid, err)
		}
		if invalid == nil {
			if pgerr.Code != "23502" || pgerr.ColumnName != "requested_publication_mode" {
				t.Fatalf("null mode returned unrelated error: %v", err)
			}
		} else if pgerr.Code != "23514" || pgerr.ConstraintName != "catalog_import_jobs_publication_mode_check" {
			t.Fatalf("invalid mode %v returned unrelated error: %v", invalid, err)
		}
	}
	if _, err := db.Exec(ctx, `update catalog_import_jobs set requested_publication_mode='automatic' where id='policy-job'`); err != nil {
		t.Fatal(err)
	}
	if err := database.Initialize(ctx, db); err != nil {
		t.Fatalf("check installed schema: %v", err)
	}
	var status string
	var creator, preservedFile int64
	if err := db.QueryRow(ctx, `select job.requested_publication_mode,job.status,job.created_by,package.archive_file_id
		from catalog_import_jobs job join catalog_import_packages package on package.id=job.package_id where job.id='policy-job'`).
		Scan(&mode, &status, &creator, &preservedFile); err != nil || mode != "automatic" || status != "queued" || creator != user || preservedFile != file {
		t.Fatalf("stored mode/status=%s/%s creator/file=%d/%d err=%v", mode, status, creator, preservedFile, err)
	}
}
