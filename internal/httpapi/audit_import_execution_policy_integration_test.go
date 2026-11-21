package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This tracer pauses a real transaction after BEGIN, before it acquires a
// publication/lease lock. Its observer owns a separate pool; the worker pool
// therefore cannot satisfy a nested acquisition using the observation path.
type auditImportBarrierContextKey struct{}

type auditImportPublicationBarrier struct {
	observer    *pgxpool.Pool
	job         string
	minProgress int
	ready       chan struct{}
	release     chan struct{}
	once        sync.Once
}

func (b *auditImportPublicationBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}
func (b *auditImportPublicationBarrier) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if ctx.Value(auditImportBarrierContextKey{}) != b.job || strings.ToLower(data.CommandTag.String()) != "begin" {
		return
	}
	var progress int
	if err := b.observer.QueryRow(ctx, `select progress from catalog_import_jobs where id=$1`, b.job).Scan(&progress); err != nil || progress < b.minProgress {
		return
	}
	b.once.Do(func() {
		close(b.ready)
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	})
}
func auditImportInstallBarrier(t *testing.T, f auditFixGRFixture, job string, size int32, minProgress int) (*auditImportPublicationBarrier, *pgxpool.Pool) {
	t.Helper()
	b := &auditImportPublicationBarrier{observer: f.db, job: job, minProgress: minProgress, ready: make(chan struct{}), release: make(chan struct{})}
	cfg := f.db.Config()
	cfg.MaxConns = size
	cfg.ConnConfig.Tracer = b
	pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.server.db = pool
	t.Cleanup(pool.Close)
	t.Cleanup(func() {
		select {
		case <-b.release:
		default:
			close(b.release)
		}
	})
	return b, pool
}
func auditImportWaitBarrier(t *testing.T, f auditFixGRFixture, b *auditImportPublicationBarrier) {
	t.Helper()
	select {
	case <-b.ready:
	case <-f.ctx.Done():
		t.Fatal("worker did not reach publication transaction barrier")
	}
}
func auditImportAssertNoProjection(t *testing.T, f auditFixGRFixture, job string) {
	t.Helper()
	if n := f.number(t, `select count(*) from catalog_import_revisions where job_id=$1`, job); n != 0 {
		t.Fatalf("cancelled/failed attempt left revisions=%d", n)
	}
	if n := f.number(t, `select count(*) from mod_resource_version_details where version_id=$1 and status='active'`, f.versionID); n != 0 {
		t.Fatalf("cancelled/failed attempt published details=%d", n)
	}
	if n := f.number(t, `select count(*) from catalog_import_job_artifacts where job_id=$1 and status in ('planned','uploaded','active')`, job); n != 0 {
		t.Fatalf("cancelled/failed attempt retained live artifacts=%d", n)
	}
}
func auditImportEmbeddedJob(t *testing.T, f auditFixGRFixture) modExportJobResponse {
	t.Helper()
	raw, err := json.Marshal(embeddedIconCatalogEntry{Name: "Policy Item", EnglishName: "Policy Item", RegisterName: "example:policy_item", Type: "Item", SmallIcon: embeddedIconTestPNG(t, 32, 32), LargeIcon: embeddedIconTestPNG(t, 32, 32)})
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	digest := sha256.Sum256(raw)
	base := "/api/v1/mods/audit-gr-" + f.modCode + "/catalog-imports"
	ticket := decodeTEST022Data[test037Ticket](t, f.require(t, f.editor, "POST", base+"/uploads/presign", map[string]any{"originalName": "policy.json", "contentType": "application/json", "source": "iconrenderer", "sizeBytes": len(raw), "sha256": hex.EncodeToString(digest[:])}, 200))
	req, err := http.NewRequestWithContext(f.ctx, ticket.Method, ticket.URL, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range ticket.Headers {
		req.Header.Set(k, v)
	}
	res, err := f.provider.Client().Do(req)
	if err != nil {
		t.Fatal("synthetic source upload failed")
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("source PUT=%d", res.StatusCode)
	}
	completed := decodeTEST022Data[map[string]any](t, f.require(t, f.editor, "POST", base+"/uploads/complete", ticket.completion(), 201))
	return decodeTEST022Data[modExportJobResponse](t, f.require(t, f.editor, "POST", base, map[string]any{"ossFileId": completed["id"], "targetVersionPublicId": f.versionPublic, "source": "iconrenderer"}, 202))
}

func TestAuditImportExecutionPolicyRechecksPublicationAndPreservesReviewModeIntegration(t *testing.T) {
	for _, format := range []string{"zip", "embedded"} {
		for _, change := range []string{"review_allowed", "automatic_allowed", "automatic_revoked", "archived", "edit_expired"} {
			t.Run(format+"/"+change, func(t *testing.T) {
				f := newAuditFixGRFixture(t)
				if strings.HasPrefix(change, "automatic_") {
					grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.no-review."+f.modCode)
				}
				generation := func() int {
					return f.number(t, `select version from catalog_dataset_state where singleton`)
				}
				var job modExportJobResponse
				if format == "zip" {
					job = f.job(t, f.upload(t, auditFixGRZip(t, nil)))
				} else {
					job = auditImportEmbeddedJob(t, f)
				}
				beforePublication := generation()
				minimum := 96
				if format == "embedded" {
					minimum = 85
				}
				b, _ := auditImportInstallBarrier(t, f, job.ID, 2, minimum)
				result := make(chan error, 1)
				go func() {
					result <- f.server.importModExportJob(context.WithValue(f.ctx, auditImportBarrierContextKey{}, job.ID), job.ID)
				}()
				auditImportWaitBarrier(t, f, b)
				switch change {
				case "automatic_revoked":
					if _, err := f.db.Exec(f.ctx, `update user_permissions grant_row set allow=false from permissions p where grant_row.user_id=$1 and p.id=grant_row.permission_id and p.code=$2`, f.userIDs[f.editor], "project.no-review."+f.modCode); err != nil {
						t.Fatal(err)
					}
				case "archived":
					if _, err := f.db.Exec(f.ctx, `update mod_content_versions set status='archived' where id=$1`, f.versionID); err != nil {
						t.Fatal(err)
					}
				case "edit_expired":
					if _, err := f.db.Exec(f.ctx, `update user_permissions grant_row set expires_at=now()-interval '1 second' from permissions p where grant_row.user_id=$1 and p.id=grant_row.permission_id and p.code=$2`, f.userIDs[f.editor], "project.edit."+f.modCode); err != nil {
						t.Fatal(err)
					}
				}
				close(b.release)
				if err := <-result; err != nil {
					t.Fatal(err)
				}
				var status, stage, code, token string
				if err := f.db.QueryRow(f.ctx, `select status,current_stage,error_code,run_token from catalog_import_jobs where id=$1`, job.ID).Scan(&status, &stage, &code, &token); err != nil {
					t.Fatal(err)
				}
				if strings.HasSuffix(change, "_allowed") {
					wantStage := "review"
					active := false
					if change == "automatic_allowed" {
						wantStage = "complete"
						active = true
					}
					expected := beforePublication
					if active {
						expected++
					}
					if got := generation(); got != expected {
						t.Fatalf("catalog generation=%d, want %d for %s", got, expected, change)
					}
					if status != "ready" || stage != wantStage || token != "" || code != "" {
						t.Fatalf("allowed state=%s/%s/%s lease=%v", status, stage, code, token != "")
					}
					if n := f.number(t, `select count(*) from catalog_import_revisions where job_id=$1 and is_active=$2 and status='ready'`, job.ID, active); n != 1 {
						t.Fatalf("allowed revision=%d active=%v", n, active)
					}
				} else {
					if status != "cancelled" || !strings.HasSuffix(code, "_reupload_required") || token != "" {
						t.Fatalf("changed eligibility state=%s/%s lease=%v", status, code, token != "")
					}
					auditImportAssertNoProjection(t, f, job.ID)
					if n := f.number(t, `select count(*) from audit_events where aggregate_key=$1 and action='execution_cancelled'`, job.ID); n != 1 {
						t.Fatalf("policy cancellation audit=%d", n)
					}
					if retried, err := f.server.resetModExportJobForRetry(f.ctx, job.ID, f.modID, f.userIDs[f.editor]); err != nil || retried {
						t.Fatalf("terminal policy cancellation retry=%v/%v", retried, err)
					}
				}
				if n := f.number(t, `select count(*) from nats_outbox where aggregate_id=$1`, job.ID); n != 1 {
					t.Fatalf("unexpected publication/retry outbox=%d", n)
				}
				if change == "archived" {
					if _, err := f.db.Exec(f.ctx, `update mod_content_versions set status='active' where id=$1`, f.versionID); err != nil {
						t.Fatal(err)
					}
					var replacement modExportJobResponse
					if format == "zip" {
						replacement = f.job(t, f.upload(t, auditFixGRZip(t, nil)))
					} else {
						replacement = auditImportEmbeddedJob(t, f)
					}
					if replacement.ID == job.ID {
						t.Fatal("fresh same-byte upload reused the policy-cancelled attempt")
					}
					if err := f.server.importModExportJob(f.ctx, replacement.ID); err != nil {
						t.Fatal(err)
					}
					if n := f.number(t, `select count(*) from catalog_import_jobs where id=$1 and status='ready' and current_stage='review'`, replacement.ID); n != 1 {
						t.Fatal("fresh upload could not complete after target restoration")
					}
					if n := f.number(t, `select count(*) from audit_events where aggregate_key=$1 and action='execution_cancelled'`, job.ID); n != 1 {
						t.Fatal("new upload changed the cancelled attempt's audit")
					}
				}
			})
		}
	}
}

func TestAuditGRR01CancelledTimedOutAndSupersededWorkersCannotCommitIntegration(t *testing.T) {
	for _, size := range []int32{2, 12} {
		for _, schedule := range []string{"cancel", "timeout", "superseded"} {
			t.Run(fmt.Sprintf("pool%d/%s", size, schedule), func(t *testing.T) {
				f := newAuditFixGRFixture(t)
				job := f.job(t, f.upload(t, auditFixGRZip(t, nil)))
				b, pool := auditImportInstallBarrier(t, f, job.ID, size, 96)
				workerCtx := f.ctx
				var cancel context.CancelFunc
				if schedule == "timeout" {
					workerCtx, cancel = context.WithTimeout(f.ctx, 2*time.Second)
				} else {
					workerCtx, cancel = context.WithCancel(f.ctx)
				}
				defer cancel()
				result := make(chan error, 1)
				go func() {
					result <- f.server.importMCModsExportJob(context.WithValue(workerCtx, auditImportBarrierContextKey{}, job.ID), job.ID)
				}()
				auditImportWaitBarrier(t, f, b)
				if pool.Stat().AcquiredConns() != 1 {
					t.Fatalf("publication barrier connections=%d", pool.Stat().AcquiredConns())
				}
				var oldToken string
				if err := f.db.QueryRow(f.ctx, `select run_token from catalog_import_jobs where id=$1`, job.ID).Scan(&oldToken); err != nil || oldToken == "" {
					t.Fatalf("active attempt token missing err=%v", err)
				}
				switch schedule {
				case "cancel":
					f.require(t, f.editor, "POST", "/api/v1/mods/audit-gr-"+f.modCode+"/export-imports/"+job.ID+"/cancel", nil, 200)
					close(b.release)
				case "timeout":
					<-workerCtx.Done() // Real deadline, not a sleep-based interleaving.
				case "superseded":
					if _, err := f.db.Exec(f.ctx, `update catalog_import_jobs set heartbeat_at=now()-interval '10 minutes',updated_at=now()-interval '10 minutes' where id=$1 and run_token=$2`, job.ID, oldToken); err != nil {
						t.Fatal(err)
					}
					if err := f.server.recoverStaleModExportJobs(f.ctx); err != nil {
						t.Fatal(err)
					}
					// Release before the replacement Worker to ensure the old attempt exits;
					// its publication query must fail the run_token fence, never mark failed.
					newToken, err := f.server.claimModExportJob(f.ctx, job.ID)
					if err != nil || newToken == oldToken {
						t.Fatalf("replacement lease=%v err=%v", newToken != oldToken, err)
					}
					close(b.release)
				}
				err := <-result
				if schedule == "timeout" {
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("timeout returned %v", err)
					}
				} else if !errors.Is(err, errModExportLeaseLost) {
					t.Fatalf("late attempt error=%v", err)
				}
				var status, token string
				if err := f.db.QueryRow(f.ctx, `select status,run_token from catalog_import_jobs where id=$1`, job.ID).Scan(&status, &token); err != nil {
					t.Fatal(err)
				}
				want := "cancelled"
				if schedule == "timeout" {
					want = "failed"
				}
				if schedule == "superseded" {
					want = "validating"
				}
				if status != want || (schedule != "superseded" && token != "") || (schedule == "superseded" && (token == "" || token == oldToken)) {
					t.Fatalf("late worker overwrote state=%s expected=%s tokenPresent=%v", status, want, token != "")
				}
				auditImportAssertNoProjection(t, f, job.ID)
				wantOutbox := 1
				if schedule == "superseded" {
					wantOutbox = 2
				}
				if n := f.number(t, `select count(*) from nats_outbox where aggregate_id=$1`, job.ID); n != wantOutbox {
					t.Fatalf("attempt outbox=%d expected=%d", n, wantOutbox)
				}
				if schedule == "superseded" {
					if _, err := f.db.Exec(f.ctx, `update catalog_import_jobs set heartbeat_at=now()-interval '10 minutes' where id=$1`, job.ID); err != nil {
						t.Fatal(err)
					}
					if err := f.server.recoverStaleModExportJobs(f.ctx); err != nil {
						t.Fatal(err)
					}
					if err := f.server.importMCModsExportJob(f.ctx, job.ID); err != nil {
						t.Fatal(err)
					}
					if n := f.number(t, `select count(*) from catalog_import_revisions where job_id=$1 and status='ready' and not is_active`, job.ID); n != 1 {
						t.Fatalf("replacement attempt could not complete exactly once: %d", n)
					}
				}
			})
		}
	}
}

func TestAuditImportPolicyUsesPublicationLockOrderIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.no-review."+f.modCode)
	first := f.job(t, f.upload(t, auditFixGRZip(t, nil)))
	second := f.job(t, f.upload(t, auditFixGRZip(t, map[string]string{"manifest.json": `{"schema_version":"mcmods-export/v1","exporter_version":"distinct-lock-order-sample","status":"complete","minecraft_version":"1.20.1","loader":"forge","package_format":"zip","configuration":{"namespaces":["example"],"profile":"all"},"stages":[],"errors":[],"error_count":0}`})))
	cfg := f.db.Config()
	cfg.MaxConns = 2
	workers, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer workers.Close()
	f.server.db = workers
	firstToken, err := f.server.claimModExportJob(f.ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondToken, err := f.server.claimModExportJob(f.ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstTx, err := workers.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer firstTx.Rollback(f.ctx)
	secondTx, err := workers.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer secondTx.Rollback(f.ctx)
	if automatic, err := catalogImportEligibilityTx(f.ctx, firstTx, first.ID, firstToken); err != nil || !automatic {
		t.Fatalf("first eligibility=%v/%v", automatic, err)
	}
	var secondPID int32
	if err := secondTx.QueryRow(f.ctx, `select pg_backend_pid()`).Scan(&secondPID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	ctx, stop := context.WithTimeout(f.ctx, 5*time.Second)
	defer stop()
	go func() {
		automatic, err := catalogImportEligibilityTx(ctx, secondTx, second.ID, secondToken)
		if err == nil && !automatic {
			err = fmt.Errorf("second eligible actor lost automatic mode")
		}
		result <- err
	}()
	for {
		select {
		case err := <-result:
			t.Fatalf("second policy escaped target serialization before first publication: %v", err)
		default:
		}
		var waiting bool
		if err := f.db.QueryRow(ctx, `select exists(select 1 from pg_stat_activity where pid=$1 and wait_event_type='Lock' and wait_event='advisory')`, secondPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
	}
	// With the old SHARE-before-advisory order, both actors can hold SHARE and
	// neither can safely upgrade. This first actor already owns the publication
	// advisory and version row, so its subsequent sync lock cannot invert order.
	if _, err := lockImportedContentVersionTx(ctx, firstTx, f.versionID); err != nil {
		t.Fatal(err)
	}
	if err := firstTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if _, err := lockImportedContentVersionTx(ctx, secondTx, f.versionID); err != nil {
		t.Fatal(err)
	}
	if err := secondTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if n := f.number(t, `select count(*) from catalog_import_revisions where job_id=any($1::text[])`, []string{first.ID, second.ID}); n != 0 {
		t.Fatalf("eligibility checks created revisions=%d", n)
	}
}
