package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOPS003CatalogImportArtifactsRegisterActivateAndCompensateAtomicallyIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify catalog import OSS compensation")
		}
		databaseURL = "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable"
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err = pool.Exec(ctx, `
		create temp table catalog_import_jobs(
			id text primary key,status text not null,progress integer not null default 0,current_stage text not null default '',
			error_code text not null default '',error_detail jsonb not null default '{}'::jsonb,
			started_at timestamptz,finished_at timestamptz,heartbeat_at timestamptz,
			run_token text not null default '',updated_at timestamptz not null default now()
		);
		create temp table oss_files(
			id bigserial primary key,bucket text not null,endpoint text not null,region text not null,
			object_key text not null unique,category text not null,source text not null,
			original_name text not null,source_original_name text not null,content_type text not null,
			size_bytes bigint not null,source_size_bytes bigint not null,sha256 text not null,
			uploader_id bigint,status text not null,scan_status text not null,updated_at timestamptz not null default now()
		);
		create temp table catalog_import_job_artifacts(
			job_id text not null,run_token text not null,object_key text not null,oss_file_id bigint not null unique,
			status text not null default 'planned',updated_at timestamptz not null default now(),
			primary key(job_id,run_token,object_key)
		);
		create temp table catalog_import_revisions(
			id text primary key,package_id text not null,mod_id bigint not null,status text not null,import_run_token text not null
		);
		create temp table log_shares(source_file_id bigint,status text,deleted_at timestamptz);
		create temp table oss_object_deletion_outbox(
			id bigserial primary key,oss_file_id bigint,bucket text not null,endpoint text not null,region text not null,
			use_cname boolean not null default false,object_key text not null,reason text not null default '',
			status text not null default 'pending',attempts integer not null default 0,max_attempts integer not null default 12,
			next_attempt_at timestamptz not null default now(),locked_at timestamptz,locked_by text not null default '',
			last_error text not null default '',failure_class text not null default '',dead_at timestamptz,deleted_at timestamptz,
			updated_at timestamptz not null default now(),unique(bucket,endpoint,object_key)
		);
		insert into catalog_import_jobs(id,status,run_token,heartbeat_at)
		values('active-job','importing','active-run',now()),
		      ('failed-job','importing','failed-run',now()),
		      ('rollback-job','importing','rollback-run',now()),
		      ('stale-job','importing','stale-run',now()-interval '10 minutes');
	`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	cfg := ossConfigPayload{Bucket: "catalog-bucket", Endpoint: "oss.example.test", Region: "cn-test", Prefix: "mcmods"}
	asset := func(job string) modExportUploadAsset {
		return modExportUploadAsset{
			ObjectKey: "mcmods/catalog/" + job + ".png", Original: job + ".png",
			Digest: strings.Repeat("a", 64), ContentType: "image/png", ByteLength: 3, Data: []byte("png"),
		}
	}

	activeFiles, err := server.registerCatalogImportArtifacts(ctx, "active-job", "active-run", cfg, 7, "mcmods_exporter", []modExportUploadAsset{asset("active")})
	if err != nil || activeFiles[asset("active").ObjectKey] <= 0 {
		t.Fatalf("pre-upload registration = %#v/%v", activeFiles, err)
	}
	assertCatalogImportArtifactState(t, ctx, pool, "active-job", "active-run", "planned", "pending", 0)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = activateCatalogImportArtifactsTx(ctx, tx, "active-job", "active-run"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertCatalogImportArtifactState(t, ctx, pool, "active-job", "active-run", "active", "active", 0)

	if _, err = server.registerCatalogImportArtifacts(ctx, "failed-job", "failed-run", cfg, 7, "mcmods_exporter", []modExportUploadAsset{asset("failed")}); err != nil {
		t.Fatal(err)
	}
	if err = server.cleanupModExportStaging(ctx, "failed-job", "package", 1, "failed-run"); err != nil {
		t.Fatal(err)
	}
	assertCatalogImportArtifactState(t, ctx, pool, "failed-job", "failed-run", "abandoned", "deleted", 1)

	if _, err = server.registerCatalogImportArtifacts(ctx, "stale-job", "stale-run", cfg, 7, "mcmods_exporter", []modExportUploadAsset{asset("stale")}); err != nil {
		t.Fatal(err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.compensateStaleCatalogImportArtifactsTx(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertCatalogImportArtifactState(t, ctx, pool, "stale-job", "stale-run", "abandoned", "deleted", 1)

	if _, err = server.registerCatalogImportArtifacts(ctx, "rollback-job", "rollback-run", cfg, 7, "mcmods_exporter", []modExportUploadAsset{asset("rollback")}); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `alter table oss_object_deletion_outbox add constraint reject_rollback_cleanup
		check(reason<>'catalog-import-failed') not valid`); err != nil {
		t.Fatal(err)
	}
	err = server.cleanupModExportStaging(ctx, "rollback-job", "package", 1, "rollback-run")
	if err == nil {
		t.Fatal("compensation persistence failure was swallowed")
	}
	assertCatalogImportArtifactState(t, ctx, pool, "rollback-job", "rollback-run", "planned", "pending", 0)
	if _, err = pool.Exec(ctx, `update catalog_import_jobs set status='failed',run_token='' where id='rollback-job'`); err != nil {
		t.Fatal(err)
	}
	recovered, err := server.recoverOrphanedCatalogImportArtifacts(ctx)
	if err != nil || !recovered {
		t.Fatalf("orphan recovery = %t/%v", recovered, err)
	}
	assertCatalogImportArtifactState(t, ctx, pool, "rollback-job", "rollback-run", "abandoned", "deleted", 1)
	if recovered, err = server.recoverOrphanedCatalogImportArtifacts(ctx); err != nil || recovered {
		t.Fatalf("idempotent orphan recovery = %t/%v", recovered, err)
	}
}

func assertCatalogImportArtifactState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	jobID string,
	runToken string,
	wantArtifact string,
	wantFile string,
	wantDeletions int,
) {
	t.Helper()
	var artifactStatus, fileStatus string
	var deletions int
	if err := pool.QueryRow(ctx, `select artifact.status,file.status
		from catalog_import_job_artifacts artifact join oss_files file on file.id=artifact.oss_file_id
		where artifact.job_id=$1 and artifact.run_token=$2`, jobID, runToken).Scan(&artifactStatus, &fileStatus); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from oss_object_deletion_outbox deletion
		join catalog_import_job_artifacts artifact on artifact.oss_file_id=deletion.oss_file_id
		where artifact.job_id=$1 and artifact.run_token=$2`, jobID, runToken).Scan(&deletions); err != nil {
		t.Fatal(err)
	}
	if artifactStatus != wantArtifact || fileStatus != wantFile || deletions != wantDeletions {
		t.Fatalf("artifact/file/deletions = %s/%s/%d, want %s/%s/%d", artifactStatus, fileStatus, deletions, wantArtifact, wantFile, wantDeletions)
	}
}

func TestOPS003CatalogImportUploadCallSitesRegisterBeforeProviderWrite(t *testing.T) {
	for _, fileName := range []string{"mod_export_handlers.go", "mod_embedded_icon_import.go"} {
		source, err := os.ReadFile(fileName)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		registration := strings.Index(text, "registerCatalogImportArtifacts(")
		providerWrite := strings.Index(text, ".submitAsset(")
		if registration < 0 || providerWrite < 0 || registration > providerWrite {
			t.Fatalf("%s does not register import artifacts before the provider write", fileName)
		}
	}
	persistence, err := os.ReadFile("mod_export_import_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(persistence), "func persistExportPNGMedia(")
	if start < 0 || strings.Contains(string(persistence)[start:], "insert into oss_files") {
		t.Fatal("PNG media persistence can still register an OSS object after upload")
	}
}
