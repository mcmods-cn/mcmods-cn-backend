package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOSSReuseMetricsAndCleanupFailuresAreObservableIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	owner := f.userIDs[f.editor]
	raw, _, err := encodeBlueprint(test036Document(), "nbt")
	if err != nil {
		t.Fatal(err)
	}
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "observable-blueprint.nbt", "application/octet-stream", "blueprint", raw)
	f.put(t, ticket, raw)
	completed := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	filePublicID := completed["id"].(string)
	blueprintPublicID := completed["blueprintId"].(string)
	objectKey := completed["objectKey"].(string)
	var fileID int64
	if err = f.db.QueryRow(f.ctx, `update oss_files set scan_status='clean' where public_id=$1 and source_hash_verified returning id`, filePublicID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	poolConfig := f.db.Config()
	poolConfig.MaxConns, poolConfig.MinConns = 1, 1
	pool, err := pgxpool.NewWithConfig(f.ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(f.ctx, 20*time.Second)
	defer cancel()
	server := &Server{db: pool}
	lookup := ossFileHashLookup{UploaderID: int64Pointer(owner), ScanStatuses: []string{"clean"}}
	file, found, err := server.findExistingOSSFileByHashExact(ctx, ticket.SHA256, ticket.SizeBytes, lookup)
	if err != nil || !found || file["id"] != filePublicID {
		t.Fatalf("healthy hash lookup = %#v/%v/%v", file, found, err)
	}
	if file, found, err = server.findExistingOSSFileByHashExact(ctx, strings.Repeat("b", 64), ticket.SizeBytes, lookup); err != nil || found || file != nil {
		t.Fatalf("missing hash lookup = %#v/%v/%v", file, found, err)
	}
	if blueprint, blueprintErr := server.reusableBlueprintByHash(ctx, ticket.SHA256, ticket.SizeBytes, owner); blueprintErr != nil || blueprint["id"] != blueprintPublicID {
		t.Fatalf("healthy blueprint reuse = %#v/%v", blueprint, blueprintErr)
	}
	request := ossCompleteUploadRequest{ObjectKey: ticket.ObjectKey, Category: ticket.Category, Source: ticket.Source, SHA256: ticket.SHA256, SizeBytes: ticket.SizeBytes}
	if id, completed, foundCompleted, completedErr := server.findCompletedOSSUpload(ctx, owner, request); completedErr != nil || !foundCompleted || id != fileID || completed["id"] != filePublicID {
		t.Fatalf("healthy completed lookup = %d/%#v/%v/%v", id, completed, foundCompleted, completedErr)
	}
	requirePGError := func(err error, code string) {
		t.Helper()
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != code {
			t.Fatalf("expected exact PostgreSQL %s, got %v", code, err)
		}
	}

	if _, err = pool.Exec(ctx, `alter table oss_files rename column sha256 to arch020_broken_sha256`); err != nil {
		t.Fatal(err)
	}
	_, _, err = server.findExistingOSSFileByHashExact(ctx, ticket.SHA256, ticket.SizeBytes, lookup)
	requirePGError(err, "42703")
	_, err = server.reusableBlueprintByHash(ctx, ticket.SHA256, ticket.SizeBytes, owner)
	requirePGError(err, "42703")
	_, _, _, err = server.findCompletedOSSUpload(ctx, owner, request)
	requirePGError(err, "42703")
	if _, err = pool.Exec(ctx, `alter table oss_files rename column arch020_broken_sha256 to sha256`); err != nil {
		t.Fatal(err)
	}

	// Full current migrations and an exact trigger fault replace the old
	// dummy-column temporary schema. A missing new relation cannot accidentally
	// satisfy a write-observability assertion.
	if _, err = pool.Exec(ctx, `create function audit_oss_observability_fault() returns trigger language plpgsql as $$ begin raise exception 'AUDIT_OSS_OBSERVABILITY_ONLY_FAILURE' using errcode='XX001'; end $$;
	 create trigger audit_oss_upload_fault before insert on oss_upload_logs for each row execute function audit_oss_observability_fault();
	 create trigger audit_oss_scan_fault before insert on oss_scan_logs for each row execute function audit_oss_observability_fault();
	 create trigger audit_oss_download_fault before insert or update on oss_download_stats for each row execute function audit_oss_observability_fault()`); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `insert into oss_download_stats(object_key,downloads) values('owned-fault-check',1)`)
	requirePGError(err, "XX001")
	server.insertOSSUploadLog(ctx, nil, owner, objectKey, "log.bin", 1, "127.0.0.1", "test", "failed", "test")
	server.recordOSSScanLog(ctx, fileID, objectKey, "pending", "test")
	server.recordOSSDownloadStat(ctx, objectKey)
	metrics := server.ossWrites.snapshot()
	if metrics.UploadLogFailures != 1 || metrics.ScanLogFailures != 1 || metrics.DownloadStatFailures != 1 {
		t.Fatalf("best-effort write metrics = %+v", metrics)
	}

	cfg := f.cfg
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
	if _, err = pool.Exec(ctx, `insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
			content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status)
		values($1,$2,$3,'objects/pending-delete.bin','misc','test','saved.bin',
			'saved.bin','application/octet-stream',1,1,repeat('c',64),$4,'active','clean')`, cfg.Bucket, cfg.Endpoint, cfg.Region, owner); err != nil {
		t.Fatal(err)
	}
	worker := &OSSDeletionWorker{server: server, workerID: "arch020-worker"}
	job, err := worker.claim(ctx)
	if err != nil || job.ObjectKey != "objects/pending-delete.bin" || job.FileID != nil {
		t.Fatalf("actual unregistered cleanup claim = %#v/%v", job, err)
	}
	if err = worker.deleteObject(ctx, job); err != nil {
		t.Fatalf("late registration guard failed: %v", err)
	}

	if _, err = pool.Exec(ctx, `alter table oss_object_deletion_outbox rename column reason to arch020_broken_reason`); err != nil {
		t.Fatal(err)
	}
	err = server.enqueueUnregisteredOSSObjectDeletion(ctx, cfg, "objects/untracked-delete.bin", "ARCH-020 exact enqueue boundary")
	requirePGError(err, "42703")
	server.deleteOSSObjectIfUnregistered(ctx, cfg, "objects/untracked-delete.bin", "ARCH-020 enqueue failure")
	metrics = server.ossWrites.snapshot()
	if metrics.DeletionEnqueueFailures != 1 {
		t.Fatalf("deletion enqueue failure metrics = %+v", metrics)
	}
}
