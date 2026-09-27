package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOSSReuseMetricsAndCleanupFailuresAreObservableIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify OSS failure observability")
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err = pool.Exec(ctx, `
		create temp table oss_files(
			id bigserial primary key,public_id text not null,bucket text not null,endpoint text not null,region text not null,
			object_key text not null unique,category text not null,source text not null,original_name text not null,
			source_original_name text not null,content_type text not null,size_bytes bigint not null,source_size_bytes bigint not null,
			sha256 text not null,uploader_id bigint,status text not null,scan_status text not null,
			created_at timestamptz not null default now(),updated_at timestamptz not null default now()
		);
		create temp table blueprints(
			id bigint primary key,public_id text not null,status text not null,original_file_id bigint,
			owner_id bigint not null,review_status text not null,created_at timestamptz not null default now()
		);
		create temp table oss_object_deletion_outbox(
			id bigserial primary key,oss_file_id bigint,bucket text not null,endpoint text not null,region text not null,
			use_cname boolean not null default false,object_key text not null,reason text not null default '',
			status text not null default 'pending',attempts integer not null default 0,max_attempts integer not null default 12,
			next_attempt_at timestamptz not null default now(),locked_at timestamptz,locked_by text not null default '',
			last_error text not null default '',failure_class text not null default '',dead_at timestamptz,deleted_at timestamptz,
			created_at timestamptz not null default now(),updated_at timestamptz not null default now(),
			unique(bucket,endpoint,object_key)
		);
		create temp table oss_upload_logs(dummy integer);
		create temp table oss_scan_logs(dummy integer);
		create temp table oss_download_stats(dummy integer);
		insert into oss_files(public_id,bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
			content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status)
		values('oss-observable-1','bucket','https://oss.example.test','region','objects/existing.bin','misc','test','existing.bin',
			'existing.bin','application/octet-stream',10,10,repeat('a',64),7,'active','clean');
		insert into blueprints values(1,'observable-blueprint','ready',1,7,'approved',now());
	`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	lookup := ossFileHashLookup{UploaderID: int64Pointer(7), ScanStatuses: []string{"clean"}}
	file, found, err := server.findExistingOSSFileByHashExact(ctx, strings.Repeat("a", 64), 10, lookup)
	if err != nil || !found || file["id"] != "oss-observable-1" {
		t.Fatalf("healthy hash lookup = %#v/%v/%v", file, found, err)
	}
	if file, found, err = server.findExistingOSSFileByHashExact(ctx, strings.Repeat("b", 64), 10, lookup); err != nil || found || file != nil {
		t.Fatalf("missing hash lookup = %#v/%v/%v", file, found, err)
	}
	if blueprint, blueprintErr := server.reusableBlueprintByHash(ctx, strings.Repeat("a", 64), 10, 7); blueprintErr != nil || blueprint["id"] != "observable-blueprint" {
		t.Fatalf("healthy blueprint reuse = %#v/%v", blueprint, blueprintErr)
	}
	request := ossCompleteUploadRequest{ObjectKey: "objects/existing.bin", Category: "misc", Source: "test", SHA256: strings.Repeat("a", 64), SizeBytes: 10}
	if id, completed, foundCompleted, completedErr := server.findCompletedOSSUpload(ctx, 7, request); completedErr != nil || !foundCompleted || id != 1 || completed["id"] != "oss-observable-1" {
		t.Fatalf("healthy completed lookup = %d/%#v/%v/%v", id, completed, foundCompleted, completedErr)
	}

	if _, err = pool.Exec(ctx, `alter table oss_files rename column sha256 to arch020_broken_sha256`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = server.findExistingOSSFileByHashExact(ctx, strings.Repeat("a", 64), 10, lookup); err == nil {
		t.Fatal("hash lookup database failure was converted to not found")
	}
	if _, err = server.reusableBlueprintByHash(ctx, strings.Repeat("a", 64), 10, 7); err == nil {
		t.Fatal("blueprint hash lookup database failure was converted to not found")
	}
	if _, _, _, err = server.findCompletedOSSUpload(ctx, 7, request); err == nil {
		t.Fatal("completed upload lookup database failure was converted to not found")
	}
	if _, err = pool.Exec(ctx, `alter table oss_files rename column arch020_broken_sha256 to sha256`); err != nil {
		t.Fatal(err)
	}

	server.insertOSSUploadLog(ctx, nil, 7, "objects/log.bin", "log.bin", 1, "127.0.0.1", "test", "failed", "test")
	server.recordOSSScanLog(ctx, 1, "objects/log.bin", "pending", "test")
	server.recordOSSDownloadStat(ctx, "objects/log.bin")
	metrics := server.ossWrites.snapshot()
	if metrics.UploadLogFailures != 1 || metrics.ScanLogFailures != 1 || metrics.DownloadStatFailures != 1 {
		t.Fatalf("best-effort write metrics = %+v", metrics)
	}

	cfg := ossConfigPayload{Bucket: "bucket", Endpoint: "https://oss.example.test", Region: "region"}
	server.deleteOSSObjectIfUnregistered(ctx, cfg, "objects/pending-delete.bin", "ARCH-020 test")
	var queued, withFile int
	if err = pool.QueryRow(ctx, `select count(*),count(oss_file_id) from oss_object_deletion_outbox where object_key='objects/pending-delete.bin'`).Scan(&queued, &withFile); err != nil {
		t.Fatal(err)
	}
	if queued != 1 || withFile != 0 {
		t.Fatalf("unregistered cleanup queue = %d rows/%d file references", queued, withFile)
	}
	server.deleteOSSObjectIfUnregistered(ctx, cfg, "objects/pending-delete.bin", "ARCH-020 repeat")
	if err = pool.QueryRow(ctx, `select count(*) from oss_object_deletion_outbox where object_key='objects/pending-delete.bin'`).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("idempotent cleanup queue count = %d/%v", queued, err)
	}
	canceledContext, cancelCleanup := context.WithCancel(ctx)
	cancelCleanup()
	server.deleteOSSObjectIfUnregistered(canceledContext, cfg, "objects/canceled-request-delete.bin", "ARCH-020 canceled request")
	if err = pool.QueryRow(ctx, `select count(*) from oss_object_deletion_outbox where object_key='objects/canceled-request-delete.bin'`).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("canceled request cleanup queue count = %d/%v", queued, err)
	}
	if _, err = pool.Exec(ctx, `insert into oss_files(public_id,bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
			content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status)
		values('oss-observable-2','bucket','https://oss.example.test','region','objects/pending-delete.bin','misc','test','saved.bin',
			'saved.bin','application/octet-stream',1,1,repeat('c',64),7,'active','clean')`); err != nil {
		t.Fatal(err)
	}
	worker := &OSSDeletionWorker{server: server, workerID: "arch020-worker"}
	if err = worker.deleteObject(ctx, ossDeletionJob{FileID: nil, Bucket: cfg.Bucket, Endpoint: cfg.Endpoint, Region: cfg.Region, ObjectKey: "objects/pending-delete.bin"}); err != nil {
		t.Fatalf("late registration guard failed: %v", err)
	}

	if _, err = pool.Exec(ctx, `alter table oss_object_deletion_outbox rename column reason to arch020_broken_reason`); err != nil {
		t.Fatal(err)
	}
	server.deleteOSSObjectIfUnregistered(ctx, cfg, "objects/untracked-delete.bin", "ARCH-020 enqueue failure")
	metrics = server.ossWrites.snapshot()
	if metrics.DeletionEnqueueFailures != 1 {
		t.Fatalf("deletion enqueue failure metrics = %+v", metrics)
	}
}
