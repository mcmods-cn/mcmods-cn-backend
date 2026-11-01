package httpapi

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Synthetic stale-worker lease fault, not a claim of a normal user path or an
// extra original audit defect. Actual upload signature expiry is never changed.
func TestAuditGR001RawLeaseRechecksDBClockAfterUnmodifiedRowWaitIntegration(t *testing.T) {
	for _, operation := range []string{"delete", "hold", "complete", "failure"} {
		t.Run(operation, func(t *testing.T) {
			base := newAuditFixGRFixture(t)
			f := newTEST037FixtureWithBase(t, base.test013Fixture)
			raw := []byte("ordinary owned lease-fault source " + operation)
			ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "lease-fence.zip", "application/zip", "test037-retention", raw)
			f.put(t, ticket, raw)
			auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201)
			auditOSSRequire(t, f, f.editor, http.MethodDelete, "/api/v1/users/me/files", map[string]any{"objectKey": ticket.ObjectKey}, 200)
			job := ossDeletionJob{Bucket: f.cfg.Bucket, Endpoint: f.cfg.Endpoint, Region: f.cfg.Region, UseCName: true, ObjectKey: ticket.ObjectKey, LockToken: "owned-raw-lease-" + randomCatalogPublicID(), Attempts: 1, MaxAttempts: 12}
			// Simulate the tail of a worker's ordinary five-minute lease. The row
			// stays unchanged while the application blocks past this real DB time.
			if err := f.db.QueryRow(f.ctx, `update oss_object_deletion_outbox set status='processing',attempts=1,locked_by=$2,locked_at=clock_timestamp()-make_interval(secs=>$3)+interval '2 seconds' where object_key=$1 and raw_upload_binding_id is not null returning id,oss_file_id,raw_upload_binding_id::text`, ticket.ObjectKey, job.LockToken, int(ossDeletionLeaseTimeout/time.Second)).Scan(&job.ID, &job.FileID, &job.RawUploadBindingID); err != nil {
				t.Fatal(err)
			}
			observerCfg := f.db.Config().Copy()
			observerCfg.MaxConns, observerCfg.MinConns = 1, 1
			observer, err := pgxpool.NewWithConfig(f.ctx, observerCfg)
			if err != nil {
				t.Fatal(err)
			}
			defer observer.Close()
			workerCfg := f.db.Config().Copy()
			workerCfg.MaxConns, workerCfg.MinConns = 2, 1
			workerPool, err := pgxpool.NewWithConfig(f.ctx, workerCfg)
			if err != nil {
				t.Fatal(err)
			}
			defer workerPool.Close()
			worker := NewOSSDeletionWorker(f.server.cfg, workerPool)
			var before string
			if err = observer.QueryRow(f.ctx, `select to_jsonb(job)::text from oss_object_deletion_outbox job where id=$1`, job.ID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			holder, err := f.db.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback(f.ctx)
			var holderPID int
			if err = holder.QueryRow(f.ctx, `select pg_backend_pid() from oss_object_deletion_outbox where id=$1 for update`, job.ID).Scan(&holderPID); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				switch operation {
				case "delete":
					done <- worker.deleteObject(f.ctx, job)
				case "complete":
					done <- worker.complete(f.ctx, job)
				case "failure":
					_, e := worker.recordFailure(f.ctx, job, errors.New("owned synthetic late provider failure"))
					done <- e
				case "hold":
					tx, e := workerPool.Begin(f.ctx)
					if e != nil {
						done <- e
						return
					}
					defer tx.Rollback(f.ctx)
					var id int64
					e = tx.QueryRow(f.ctx, `select id from oss_object_deletion_outbox where id=$1 for update`, job.ID).Scan(&id)
					if e == nil {
						e = deferOSSRawDeletionTx(f.ctx, tx, job, "raw_capability_unknown", nil)
					}
					done <- e
				}
			}()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			wait := func(query string, args ...any) {
				t.Helper()
				for {
					var yes bool
					if e := observer.QueryRow(f.ctx, query, args...).Scan(&yes); e != nil {
						t.Fatal(e)
					}
					if yes {
						return
					}
					select {
					case <-ticker.C:
					case <-f.ctx.Done():
						t.Fatal("exact raw lease barrier was not reached")
					}
				}
			}
			wait(`select exists(select 1 from pg_stat_activity where datname=current_database() and $1=any(pg_blocking_pids(pid)) and query like '%oss_object_deletion_outbox%')`, holderPID)
			wait(`select clock_timestamp()>=locked_at+make_interval(secs=>$2) from oss_object_deletion_outbox where id=$1`, job.ID, int(ossDeletionLeaseTimeout/time.Second))
			if err = holder.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			if err = <-done; !errors.Is(err, errOSSDeletionLeaseLost) {
				var state string
				_ = observer.QueryRow(f.ctx, `select status from oss_object_deletion_outbox where id=$1`, job.ID).Scan(&state)
				t.Fatalf("late %s after unchanged-row expiry reached wrong result: %v queueStatus=%s", operation, err, state)
			}
			var after string
			if err = observer.QueryRow(f.ctx, `select to_jsonb(job)::text from oss_object_deletion_outbox job where id=$1`, job.ID).Scan(&after); err != nil || after != before {
				t.Fatalf("expired original token changed queue during %s: %v", operation, err)
			}
			t.Logf("Controlled %s waiter observed via exact holderPID; unchanged queue lease crossed DB expiry before release; original token rejected, no queue/HOLD/completed/provider side effect. Workerpool2; signature deadlines unchanged.", operation)
		})
	}
}
