package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type oct03DeletionCompletePause struct {
	reached chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (pause *oct03DeletionCompletePause) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "update oss_object_deletion_outbox set") && strings.Contains(data.SQL, "status='completed'") {
		pause.once.Do(func() {
			close(pause.reached)
			select {
			case <-pause.resume:
			case <-ctx.Done():
			}
		})
	}
	return ctx
}

func (*oct03DeletionCompletePause) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// Exercise the missing OPS-015 ordering with the real PostgreSQL schema,
// application claim/recovery/deletion functions, and an actual SDK HTTP PUT.
// The provider is owned loopback storage; no cloud account or key is used.
func TestOCT03BlueprintLatePutAfterCompletedDeletionRequeuesCompensationIntegration(t *testing.T) {
	for _, scenario := range []string{"expired lease", "deleted job", "deleted blueprint", "processing deletion acknowledgement", "processing acknowledgement write failure"} {
		t.Run(scenario, func(t *testing.T) { testOCT03LateBlueprintUpload(t, scenario) })
	}
}

func testOCT03LateBlueprintUpload(t *testing.T, scenario string) {
	requireOCT02DatabaseIntegration(t)
	f := newTEST036Fixture(t)
	document := test036Document()
	raw, _, err := encodeBlueprint(document, "nbt")
	if err != nil {
		t.Fatal(err)
	}
	upload := f.upload(t, f.editor, "oct03-late-put.nbt", raw)
	worker := f.worker("late-put")
	job, claimed, err := worker.claimBlueprintJob(f.ctx, upload.jobID)
	if err != nil || !claimed {
		t.Fatalf("initial production claim: claimed=%t error=%v", claimed, err)
	}
	normalized, err := encodeNormalizedBlueprintJSON(document)
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var storageMu sync.Mutex
	objects := make(map[string][]byte)
	puts, deletes := 0, 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/test036-owned/")
		switch r.Method {
		case http.MethodPut:
			data, readErr := io.ReadAll(io.LimitReader(r.Body, maxBlueprintNormalizedBytes+1))
			if readErr != nil || len(data) > maxBlueprintNormalizedBytes {
				w.WriteHeader(400)
				return
			}
			storageMu.Lock()
			puts++
			storageMu.Unlock()
			close(started)
			// The delete completes before this already accepted request writes.
			<-release
			storageMu.Lock()
			objects[key] = bytes.Clone(data)
			storageMu.Unlock()
			w.Header().Set("ETag", `"oct03-owned-late-upload"`)
			w.WriteHeader(200)
		case http.MethodDelete:
			storageMu.Lock()
			delete(objects, key)
			deletes++
			storageMu.Unlock()
			w.WriteHeader(204)
		default:
			w.WriteHeader(405)
		}
	}))
	t.Cleanup(provider.Close)
	t.Cleanup(unblock)
	cfg := f.server.ossConfigFromSettings(f.ctx)
	originalConfig := cfg
	cfg.Endpoint, cfg.PublicEndpoint = provider.URL, provider.URL
	sealed, err := f.server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `update system_settings set value=$1::jsonb where key='oss.aliyun'`, sealed); err != nil {
		t.Fatal(err)
	}
	key := blueprintArtifactObjectKey(cfg.Prefix, ossBlueprintReleaseCategory(upload.publicID, "normalized"), job.id, job.attempts, blueprintArtifactNormalized, "json")
	type uploadResult struct {
		artifact blueprintArtifact
		err      error
	}
	finished := make(chan uploadResult, 1)
	go func() {
		artifact, uploadErr := worker.writePendingBlueprintArtifact(f.ctx, job.id, job.attempts, job.runToken,
			blueprintArtifactNormalized, key, "blueprint.json", "application/json", normalized, job.createdBy, "blueprint_normalized")
		finished <- uploadResult{artifact, uploadErr}
	}()
	select {
	case <-started:
	case <-f.ctx.Done():
		t.Fatal("owned PUT did not start before test deadline")
	}
	if scenario == "expired lease" || strings.HasPrefix(scenario, "processing") {
		if _, err = f.db.Exec(f.ctx, `update blueprint_jobs set lease_expires_at=now()-interval '1 second' where id=$1`, job.id); err != nil {
			t.Fatal(err)
		}
		recovered, exhausted, recoveryErr := f.worker("recovery").recoverStaleBlueprintJobs(f.ctx)
		if recoveryErr != nil || recovered != 1 || exhausted != 0 {
			t.Fatalf("production lease recovery=%d/%d error=%v", recovered, exhausted, recoveryErr)
		}
	} else {
		deleteSQL, deletedID := `delete from blueprint_jobs where id=$1`, job.id
		if scenario == "deleted blueprint" {
			deleteSQL, deletedID = `delete from blueprints where id=$1`, job.blueprintID
		}
		if _, err = f.db.Exec(f.ctx, deleteSQL, deletedID); err != nil {
			t.Fatal(err)
		}
		recovered, recoveryErr := f.worker("orphan-recovery").recoverOrphanedBlueprintArtifacts(f.ctx)
		if recoveryErr != nil || recovered != 1 {
			t.Fatalf("production cascade recovery=%d error=%v", recovered, recoveryErr)
		}
	}
	deletion := NewOSSDeletionWorker(f.server.cfg, f.db)
	var oldDeletion ossDeletionJob
	var oldCompletion <-chan error
	var resumeCompletion func()
	if strings.HasPrefix(scenario, "processing") {
		pause := &oct03DeletionCompletePause{reached: make(chan struct{}), resume: make(chan struct{})}
		var resumeOnce sync.Once
		resumeCompletion = func() { resumeOnce.Do(func() { close(pause.resume) }) }
		poolConfig := f.db.Config()
		poolConfig.ConnConfig.Tracer = pause
		pool, poolErr := pgxpool.NewWithConfig(f.ctx, poolConfig)
		if poolErr != nil {
			t.Fatal(poolErr)
		}
		t.Cleanup(pool.Close)
		t.Cleanup(resumeCompletion)
		deletion = NewOSSDeletionWorker(f.server.cfg, pool)
		oldDeletion, err = deletion.claim(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = deletion.deleteObject(f.ctx, oldDeletion); err != nil {
			t.Fatal(err)
		}
		completed := make(chan error, 1)
		oldCompletion = completed
		go func() { completed <- deletion.complete(f.ctx, oldDeletion) }()
		select {
		case <-pause.reached:
		case <-f.ctx.Done():
			t.Fatal("old deletion acknowledgement did not reach SQL barrier")
		}
	} else {
		deletion.drain(f.ctx)
	}
	var deletionStatus string
	expectedStatus := "completed"
	if oldCompletion != nil {
		expectedStatus = "processing"
	}
	if err = f.db.QueryRow(f.ctx, `select status from oss_object_deletion_outbox where object_key=$1`, key).Scan(&deletionStatus); err != nil || deletionStatus != expectedStatus {
		t.Fatalf("delete must actually complete before PUT: status=%s error=%v", deletionStatus, err)
	}
	storageMu.Lock()
	initialDeletes := deletes
	storageMu.Unlock()
	if initialDeletes != 1 {
		t.Fatalf("owned provider DELETE calls=%d want 1 before releasing PUT", initialDeletes)
	}
	var beforeFailedReset map[string]string
	if scenario == "processing acknowledgement write failure" {
		if _, err = f.db.Exec(f.ctx, `alter table oss_object_deletion_outbox add constraint oct03_reject_processing_reset check(status<>'pending') not valid`); err != nil {
			t.Fatal(err)
		}
		beforeFailedReset = f.facts(t)
	}
	unblock()
	var result uploadResult
	select {
	case result = <-finished:
	case <-f.ctx.Done():
		t.Fatal("late PUT did not finish before test deadline")
	}
	if !errors.Is(result.err, errBlueprintJobLeaseLost) {
		t.Errorf("late upload must reject lost lease, got %v", result.err)
	}
	if beforeFailedReset != nil {
		var databaseErr *pgconn.PgError
		if !errors.As(result.err, &databaseErr) || databaseErr.Code != "23514" {
			t.Fatalf("processing reset SQL failure must be observable: %v", result.err)
		}
		f.unchanged(t, beforeFailedReset)
		if _, err = f.db.Exec(f.ctx, `alter table oss_object_deletion_outbox drop constraint oct03_reject_processing_reset`); err != nil {
			t.Fatal(err)
		}
		if err = worker.verifyBlueprintArtifactUpload(f.ctx, result.artifact, job.runToken); !errors.Is(err, errBlueprintJobLeaseLost) || errors.As(err, &databaseErr) {
			t.Fatalf("explicit processing reset retry must only report the lost lease: %v", err)
		}
	}
	var artifactStatus, fileStatus string
	var deletionCount int
	if err = f.db.QueryRow(f.ctx, `select artifact.status,file.status,
		(select count(*) from oss_object_deletion_outbox where object_key=$1)
		from blueprint_job_artifacts artifact join oss_files file on file.id=artifact.file_id
		where artifact.id=$2`, key, result.artifact.id).Scan(&artifactStatus, &fileStatus, &deletionCount); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(f.ctx, `select status from oss_object_deletion_outbox where object_key=$1`, key).Scan(&deletionStatus); err != nil {
		t.Fatal(err)
	}
	if artifactStatus != "abandoned" || fileStatus != "deleted" || deletionCount != 1 || deletionStatus != "pending" {
		t.Fatalf("late upload compensation=%s/%s/%s count=%d want abandoned/deleted/pending/1", artifactStatus, fileStatus, deletionStatus, deletionCount)
	}
	if oldCompletion != nil {
		resumeCompletion()
		select {
		case completionErr := <-oldCompletion:
			if !errors.Is(completionErr, errOSSDeletionLeaseLost) {
				t.Fatalf("old deletion acknowledgement must be fenced: %v", completionErr)
			}
		case <-f.ctx.Done():
			t.Fatal("old deletion acknowledgement did not finish")
		}
		before := f.facts(t)
		if _, failureErr := deletion.recordFailure(f.ctx, oldDeletion, errors.New("old executor failure")); !errors.Is(failureErr, errOSSDeletionLeaseLost) {
			t.Fatalf("old deletion failure must be fenced: %v", failureErr)
		}
		f.unchanged(t, before)
	}
	deletion.drain(f.ctx)
	storageMu.Lock()
	_, exists := objects[key]
	finalPuts, finalDeletes := puts, deletes
	storageMu.Unlock()
	if exists || finalPuts != 1 || finalDeletes != 2 {
		t.Fatalf("late uploaded bytes remain=%t PUT=%d DELETE=%d want false/1/2", exists, finalPuts, finalDeletes)
	}
	if _, _, err = f.worker("second-recovery").recoverStaleBlueprintJobs(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(f.ctx, `select status from oss_object_deletion_outbox where object_key=$1`, key).Scan(&deletionStatus); err != nil || deletionStatus != "completed" {
		t.Fatalf("second cleanup must be durable and final: status=%s error=%v", deletionStatus, err)
	}
	if scenario != "expired lease" {
		return
	}
	before := f.facts(t)
	wrongKey := result.artifact
	wrongKey.objectKey = "another-attempt-must-not-reopen"
	if err = worker.abandonBlueprintArtifact(f.ctx, wrongKey, "wrong-key"); !errors.Is(err, errBlueprintArtifactState) {
		t.Fatalf("a mismatched key must not reopen compensation: %v", err)
	}
	f.unchanged(t, before)
	if _, err = f.db.Exec(f.ctx, `update oss_object_deletion_outbox set
		oss_file_id=(select original_file_id from blueprints where id=$2),status='processing',
		locked_by='another-file-incarnation',locked_at=now() where object_key=$1`, key, job.blueprintID); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	if err = worker.verifyBlueprintArtifactUpload(f.ctx, result.artifact, job.runToken); !errors.Is(err, errBlueprintJobLeaseLost) || !errors.Is(err, errBlueprintArtifactState) {
		t.Fatalf("another file's processing token must not be reset: %v", err)
	}
	f.unchanged(t, before)
	if _, err = f.db.Exec(f.ctx, `update oss_object_deletion_outbox set
		oss_file_id=$2,status='completed',locked_by='',locked_at=null where object_key=$1`, key, result.artifact.fileID); err != nil {
		t.Fatal(err)
	}
	// A compensation write failure must remain observable and must not partially
	// change the completed outbox/file/artifact. An explicit retry can then use
	// the identical key and row after the fault is removed.
	if _, err = f.db.Exec(f.ctx, `alter table oss_object_deletion_outbox add constraint oct03_reject_reopen check(status<>'pending') not valid`); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	guardErr := worker.verifyBlueprintArtifactUpload(f.ctx, result.artifact, job.runToken)
	var databaseErr *pgconn.PgError
	if !errors.Is(guardErr, errBlueprintJobLeaseLost) || !errors.As(guardErr, &databaseErr) || databaseErr.Code != "23514" {
		t.Fatalf("compensation failure must preserve lease loss and SQL error: %v", guardErr)
	}
	f.unchanged(t, before)
	if _, err = f.db.Exec(f.ctx, `alter table oss_object_deletion_outbox drop constraint oct03_reject_reopen`); err != nil {
		t.Fatal(err)
	}
	cancelledCtx, cancel := context.WithCancel(f.ctx)
	cancel()
	if guardErr = worker.verifyBlueprintArtifactUpload(cancelledCtx, result.artifact, job.runToken); !errors.Is(guardErr, errBlueprintJobLeaseLost) || errors.As(guardErr, &databaseErr) || errors.Is(guardErr, context.Canceled) {
		t.Fatalf("explicit compensation retry should only report the lost original lease: %v", guardErr)
	}
	if err = f.db.QueryRow(f.ctx, `select status from oss_object_deletion_outbox where object_key=$1`, key).Scan(&deletionStatus); err != nil || deletionStatus != "pending" {
		t.Fatalf("compensation retry=%s error=%v", deletionStatus, err)
	}
	deletion.drain(f.ctx)
	sealed, err = f.server.sealSystemSetting(originalConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `update system_settings set value=$1::jsonb where key='oss.aliyun'`, sealed); err != nil {
		t.Fatal(err)
	}
	if err = f.worker("new-valid-attempt").processJob(f.ctx, job.id); err != nil {
		t.Fatal("legitimate recovered attempt failed:", err)
	}
	var active blueprintArtifact
	if err = f.db.QueryRow(f.ctx, `select id,file_id,job_id,attempt,role,object_key from blueprint_job_artifacts
		where job_id=$1 and role='normalized' and status='active'`, job.id).Scan(&active.id, &active.fileID, &active.jobID, &active.attempt, &active.role, &active.objectKey); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	if err = worker.abandonBlueprintArtifact(f.ctx, active, "must-not-delete-active-artifact"); !errors.Is(err, errBlueprintArtifactState) {
		t.Fatalf("active artifact compensation must be refused: %v", err)
	}
	f.unchanged(t, before)
}
