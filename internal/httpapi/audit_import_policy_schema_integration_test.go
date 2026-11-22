package httpapi

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"testing"
	"time"

	"mcmods-cn-backend/internal/database"
)

func TestAuditImportPolicyFreshAndExistingMigrationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := newTEST018IsolatedDatabase(t, ctx)
	if err := database.RequireCatalogImportPolicySchema(ctx, db); err != nil {
		t.Fatalf("fresh schema: %v", err)
	}
	var user, mod, version, file int64
	if err := db.QueryRow(ctx, `insert into users(username,email,password_hash) values('policy-migration-owner','policy-migration@example.invalid','synthetic-only') returning id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,submitted_by) values($1,'policy-migration','Policy migration',$2) returning id`, randomCatalogPublicID(), user).Scan(&mod); err != nil {
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
	// Recreate only the exact pre-extension boundary, in this owned disposable
	// database, then retain a real queued job while adding the column.
	if _, err := db.Exec(ctx, `alter table catalog_import_jobs drop column requested_publication_mode`); err != nil {
		t.Fatal(err)
	}
	if err := database.Initialize(ctx, db); err == nil || !strings.Contains(err.Error(), "catalog import policy extension required") {
		t.Fatalf("old schema startup did not return the specific execution-policy preflight error: %v", err)
	}
	if _, err := db.Exec(ctx, `insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version,created_by) values('legacy-policy-job',$1,'policy-package',$2,'fixture/v1',$3)`, mod, version, user); err != nil {
		t.Fatal(err)
	}
	apply := func() {
		t.Helper()
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		for _, sql := range database.CatalogImportPolicySchemaStatements() {
			if _, err = tx.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
		}
		if err = database.RequireCatalogImportPolicySchema(ctx, tx); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	apply()
	var mode, status string
	var creator, preservedFile int64
	if err := db.QueryRow(ctx, `select job.requested_publication_mode,job.status,job.created_by,package.archive_file_id from catalog_import_jobs job join catalog_import_packages package on package.id=job.package_id where job.id='legacy-policy-job'`).Scan(&mode, &status, &creator, &preservedFile); err != nil || mode != "legacy_unknown" || status != "queued" || creator != user || preservedFile != file {
		t.Fatalf("legacy preservation mode/status=%s/%s creator/file=%d/%d err=%v", mode, status, creator, preservedFile, err)
	}
	if _, err := db.Exec(ctx, `insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version,created_by,overwrite_existing) values('new-policy-job',$1,'policy-package',$2,'fixture/v1',$3,true)`, mod, version, user); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `select requested_publication_mode from catalog_import_jobs where id='new-policy-job'`).Scan(&mode); err != nil || mode != "review" {
		t.Fatalf("new default=%s err=%v", mode, err)
	}
	if _, err := db.Exec(ctx, `update catalog_import_jobs set requested_publication_mode='automatic' where id='new-policy-job'`); err != nil {
		t.Fatal(err)
	}
	apply()
	if err := db.QueryRow(ctx, `select requested_publication_mode from catalog_import_jobs where id='new-policy-job'`).Scan(&mode); err != nil || mode != "automatic" {
		t.Fatalf("repeat changed recorded mode=%s err=%v", mode, err)
	}
	if _, err := db.Exec(ctx, `update catalog_import_jobs set requested_publication_mode='unexpected' where id='new-policy-job'`); err == nil {
		t.Fatal("invalid queue mode accepted")
	} else {
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "23514" || pgerr.ConstraintName != "catalog_import_jobs_publication_mode_check" {
			t.Fatalf("invalid mode returned unrelated error: %v", err)
		}
	}
	if err := database.Initialize(ctx, db); err != nil {
		t.Fatalf("upgraded schema startup: %v", err)
	}
	if _, err := db.Exec(ctx, `alter table catalog_import_jobs alter column requested_publication_mode drop not null`); err != nil {
		t.Fatal(err)
	}
	if err := database.RequireCatalogImportPolicySchema(ctx, db); err == nil || !strings.Contains(err.Error(), "catalog import policy extension required") {
		t.Fatalf("nullable mode column did not return the specific preflight error: %v", err)
	}
}
