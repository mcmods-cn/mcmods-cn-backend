package httpapi

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type auditImportReviewWorkerMarker struct{}
type auditImportReviewQueryMarker struct{}

// Stop an actual business Worker immediately after it owns the common advisory
// lock. Let a real HTTP reviewer contend, observe its actual database wait, then
// resume the Worker. This exposes row->advisory / advisory->row inversion without
// relying on a lucky sleep or replacing the application lock implementation.
type auditImportReviewWorkerBarrier struct {
	acquired chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (b *auditImportReviewWorkerBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if ctx.Value(auditImportReviewWorkerMarker{}) == true && strings.Contains(data.SQL, "pg_advisory_xact_lock") && strings.Contains(data.SQL, "mod-content-import:") {
		return context.WithValue(ctx, auditImportReviewQueryMarker{}, true)
	}
	return ctx
}

func (b *auditImportReviewWorkerBarrier) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err != nil || ctx.Value(auditImportReviewQueryMarker{}) != true {
		return
	}
	b.once.Do(func() {
		close(b.acquired)
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	})
}

func auditImportReviewPool(t *testing.T, f auditFixGRFixture, tracer pgx.QueryTracer) *pgxpool.Pool {
	t.Helper()
	cfg := f.db.Config()
	cfg.MaxConns = 4
	cfg.ConnConfig.RuntimeParams["application_name"] = "mcmods-audit-import-review-race"
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func auditImportReviewWaiters(t *testing.T, f auditFixGRFixture, want int) {
	t.Helper()
	ctx, stop := context.WithTimeout(f.ctx, 5*time.Second)
	defer stop()
	for {
		var n int
		if err := f.db.QueryRow(ctx, `select count(*) from pg_stat_activity where datname=current_database() and application_name='mcmods-audit-import-review-race' and wait_event_type='Lock'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n >= want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("observed %d actual review waiters; want %d", n, want)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestAuditImportReviewerAndActualWorkerUseOneLockOrderIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	_, revision := f.imported(t, auditRecipeNegativePackage(t, "valid"))
	next := f.job(t, f.upload(t, auditRecipeNegativePackage(t, "valid-second")))
	barrier := &auditImportReviewWorkerBarrier{acquired: make(chan struct{}), release: make(chan struct{})}
	var resume sync.Once
	release := func() { resume.Do(func() { close(barrier.release) }) }
	t.Cleanup(release)
	pool := auditImportReviewPool(t, f, barrier)
	f.server.db = pool
	ctx, stop := context.WithTimeout(context.WithValue(f.ctx, auditImportReviewWorkerMarker{}, true), 15*time.Second)
	defer stop()
	worker := make(chan error, 1)
	go func() { worker <- f.server.importMCModsExportJob(ctx, next.ID) }()
	select {
	case <-barrier.acquired:
	case <-ctx.Done():
		t.Fatal("actual Worker did not reach its advisory lock")
	}
	review := auditCompletionAsyncJSON(f.test013Fixture, f.reviewer, "PATCH", "/api/v1/admin/export-revisions/"+revision+"/activate", map[string]any{"status": "approved", "note": "Controlled Worker/reviewer lock ordering"})
	auditImportReviewWaiters(t, f, 1)
	release()
	workerErr := <-worker
	result := <-review
	if workerErr != nil || result.err != nil || result.status != 200 {
		t.Fatalf("common lock schedule failed: Worker=%v reviewerHTTP=%d transport=%v body=%s", workerErr, result.status, result.err, result.raw)
	}
	if f.number(t, `select count(*) from catalog_import_revisions where id=$1 and is_active and status='ready'`, revision) != 1 ||
		f.number(t, `select count(*) from catalog_import_jobs where id=$1 and status='ready' and current_stage='review'`, next.ID) != 1 ||
		f.number(t, `select count(*) from audit_events where aggregate_key=$1 and action='review_approved'`, revision) != 1 {
		t.Fatal("successful race has inconsistent publication, pending next attempt or decision audit")
	}
	t.Log("actual Worker owns advisory -> reviewer waits -> Worker completes -> reviewer approves; no deadlock/500; next revision remains review-bound")
}

func TestAuditImportApproveThenRejectRacePreservesPublicationIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	_, revision := f.imported(t, auditRecipeNegativePackage(t, "valid"))
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "content.review")
	pool := auditImportReviewPool(t, f, nil)
	f.server.db = pool
	holder, err := pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.Background())
	if _, err := lockImportedContentVersionTx(f.ctx, holder, f.versionID); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/export-revisions/" + revision + "/activate"
	approve := auditCompletionAsyncJSON(f.test013Fixture, f.reviewer, "PATCH", path, map[string]any{"status": "approved"})
	auditImportReviewWaiters(t, f, 1)
	reject := auditCompletionAsyncJSON(f.test013Fixture, f.denied, "PATCH", path, map[string]any{"status": "rejected"})
	auditImportReviewWaiters(t, f, 2)
	if err := holder.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	accepted, denied := <-approve, <-reject
	if accepted.err != nil || denied.err != nil || accepted.status != 200 || denied.status != 409 {
		t.Fatalf("ordered independent reviewers: approve=%d/%v reject=%d/%v", accepted.status, accepted.err, denied.status, denied.err)
	}
	if f.number(t, `select count(*) from catalog_import_revisions where id=$1 and status='ready' and is_active`, revision) != 1 ||
		f.number(t, `select count(*) from audit_events where aggregate_key=$1 and action='review_approved'`, revision) != 1 ||
		f.number(t, `select count(*) from audit_events where aggregate_key=$1 and action='review_rejected'`, revision) != 0 {
		t.Fatal("rejected transition changed published state or created a false rejection audit")
	}
	f.require(t, "", "GET", "/api/v1/export-revisions/"+revision, nil, 200)
	f.require(t, "", "GET", "/api/v1/export-revisions/"+revision+"/assets", nil, 200)
	t.Log("first independent approval200; queued rejection409; publication/assets/approval audit preserved")
}
