package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/activity"
)

func auditOSSAbortRequest(ticket test037Ticket) map[string]any {
	body := ticket.completion()
	body["multipartUploadId"] = ticket.Multipart.UploadID
	body["multipartAction"] = "abort"
	return body
}
func auditOSSExpireMultipartLease(t *testing.T, f test037Fixture, objectKey string) {
	t.Helper()
	tag, err := f.db.Exec(f.ctx, `update oss_multipart_sessions set lease_expires_at=now()-interval '1 second' where object_key=$1 and status='aborting'`, objectKey)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("controlled lease expiry affected=%d err=%v", tag.RowsAffected(), err)
	}
}
func auditOSSReadAbortIntent(t *testing.T, f test037Fixture, objectKey string) (string, ossMultipartAbortIntent, int, int) {
	t.Helper()
	var status string
	var raw []byte
	var reservations, intents int
	err := f.db.QueryRow(f.ctx, `select status,abort_activity_intent,(select count(*) from oss_user_upload_quota_reservations where object_key=$1),(select count(*) from activity_event_outbox where user_id=$2 and action_id=$3 and object_type_id=$4) from oss_multipart_sessions where object_key=$1`, objectKey, f.userIDs[f.editor], activity.ActionUpload, activity.ObjectFile).Scan(&status, &raw, &reservations, &intents)
	if err != nil {
		t.Fatal(err)
	}
	var intent ossMultipartAbortIntent
	if err = json.Unmarshal(raw, &intent); err != nil {
		t.Fatal(err)
	}
	if intent.Version != 1 || intent.Kind != "oss_multipart_abort" || intent.ID == "" || intent.Event.UserID != f.userIDs[f.editor] || intent.Event.ActionID != activity.ActionUpload || intent.Event.ObjectTypeID != activity.ObjectFile {
		t.Fatal("cancellation lost its original typed owner/action/identity")
	}
	return status, intent, reservations, intents
}
func auditOSSAssertAbortFinal(t *testing.T, f test037Fixture, ticket test037Ticket, eventID string, wantIntents int) {
	t.Helper()
	status, intent, reservations, intents := auditOSSReadAbortIntent(t, f, ticket.ObjectKey)
	if status != "aborted" || !intent.Queued || intent.ID != eventID || reservations != 0 || intents != wantIntents {
		t.Fatalf("cancelled state=%s queued=%v stableID=%v reservation/intents=%d/%d want=%d", status, intent.Queued, intent.ID == eventID, reservations, intents, wantIntents)
	}
	var pendingBlueprints, files int
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from blueprints where owner_id=$1 and original_object_key=$2),(select count(*) from oss_files where object_key=$2)`, f.userIDs[f.editor], ticket.ObjectKey).Scan(&pendingBlueprints, &files); err != nil || pendingBlueprints+files != 0 {
		t.Fatalf("cancelled blueprint/files=%d/%d err=%v", pendingBlueprints, files, err)
	}
	f.store.mu.Lock()
	_, exists := f.store.uploads[ticket.Multipart.UploadID]
	f.store.mu.Unlock()
	if exists {
		t.Fatal("terminal cancellation left provider multipart upload")
	}
}

func TestAuditCMP003OSSMultipartAbortCrashBeforeAfterRemoteAndLateHTTPAreFencedIntegration(t *testing.T) {
	for _, point := range []string{"before_remote", "after_remote"} {
		t.Run(point, func(t *testing.T) {
			f := newTEST037Fixture(t)
			singleConfig := f.db.Config()
			singleConfig.MaxConns, singleConfig.MinConns = 1, 1
			single, err := pgxpool.NewWithConfig(f.ctx, singleConfig)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(single.Close)
			f.server.db = single
			// No parts are uploaded: the real provider multipart session and private
			// blueprint/reservation are the facts a cancellation must settle.
			ticket := decodeTEST022Data[test037Ticket](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", ossDirectUploadRequest{OriginalName: "owned-cancel.nbt", ContentType: "application/octet-stream", SizeBytes: ossMultipartThreshold, SHA256: strings.Repeat("a", 64), Category: "test037", Source: "blueprint"}, 200))
			before := auditOSSAllFacts(t, f)
			auditOSSRequire(t, f, f.otherEditor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", auditOSSAbortRequest(ticket), 400)
			if after := auditOSSAllFacts(t, f); !reflect.DeepEqual(before, after) {
				t.Fatal("cross-owner abort changed upload facts")
			}
			handler := f.provider.Config.Handler
			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			var selected atomic.Bool
			f.provider.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete && r.URL.Query().Get("uploadId") == ticket.Multipart.UploadID && selected.CompareAndSwap(false, true) {
					if point == "after_remote" {
						handler.ServeHTTP(w, r)
					}
					close(started)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					if point == "before_remote" {
						handler.ServeHTTP(w, r)
					}
					return
				}
				handler.ServeHTTP(w, r)
			})
			response := auditCompletionAsyncJSON(f.test013Fixture, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", auditOSSAbortRequest(ticket))
			select {
			case <-started:
			case <-time.After(10 * time.Second):
				t.Fatal("actual abort did not reach controlled provider barrier")
			}
			status, intent, reservations, intents := auditOSSReadAbortIntent(t, f, ticket.ObjectKey)
			if status != "aborting" || intent.Queued || reservations != 1 || intents != 1 {
				t.Fatalf("before restart status=%s queued=%v reservations/intents=%d/%d", status, intent.Queued, reservations, intents)
			}
			// The HTTP server has exactly one slot, while the assertions use
			// their separate fixture pool. Acquiring that sole slot while the
			// provider is blocked proves the request holds no transaction;
			// unrelated response logging cannot contaminate a global count.
			probeCtx, probeCancel := context.WithTimeout(f.ctx, 2*time.Second)
			connection, probeErr := single.Acquire(probeCtx)
			probeCancel()
			if probeErr != nil {
				t.Fatalf("provider wait retains the sole HTTP database slot: %v", probeErr)
			}
			connection.Release()
			// The expired persisted lease models loss of the first process. A genuinely
			// new business worker wins, while the blocked old HTTP handler remains late.
			auditOSSExpireMultipartLease(t, f, ticket.ObjectKey)
			restarted := NewOSSMultipartCleanupWorker(f.server.cfg, f.db)
			restarted.drain(f.ctx)
			auditOSSAssertAbortFinal(t, f, ticket, intent.ID, 2)
			unblock()
			result := <-response
			if result.err != nil || result.status != 500 || !strings.Contains(string(result.raw), "保存 OSS 分片取消状态失败") {
				t.Fatalf("late HTTP result=%d err=%v", result.status, result.err)
			}
			auditOSSAssertAbortFinal(t, f, ticket, intent.ID, 2)
			auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", auditOSSAbortRequest(ticket), 200)
			restarted.drain(f.ctx)
			auditOSSAssertAbortFinal(t, f, ticket, intent.ID, 2)
		})
	}
}

func TestAuditCMP003OSSMultipartAbortUnavailableActivityOrProviderRecoversOriginalIntentIntegration(t *testing.T) {
	for _, fault := range []string{"activity", "provider"} {
		t.Run(fault, func(t *testing.T) {
			f := newTEST037Fixture(t)
			ticket := decodeTEST022Data[test037Ticket](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", ossDirectUploadRequest{OriginalName: "owned-recover.nbt", ContentType: "application/octet-stream", SizeBytes: ossMultipartThreshold, SHA256: strings.Repeat("b", 64), Category: "test037", Source: "blueprint"}, 200))
			want := 500
			if fault == "activity" {
				auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
			} else {
				f.store.failAbort.Store(true)
				want = 502
			}
			raw := auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", auditOSSAbortRequest(ticket), want)
			message := "保存 OSS 分片取消状态失败"
			if fault == "provider" {
				message = "取消 OSS 分片上传失败"
			}
			if !strings.Contains(string(raw), message) {
				t.Fatal("abort failed at an unrelated boundary")
			}
			status, intent, reservations, intents := auditOSSReadAbortIntent(t, f, ticket.ObjectKey)
			wantState := "aborting"
			if fault == "provider" {
				wantState = "cleanup_pending"
			}
			if status != wantState || intent.Queued || reservations != 1 || intents != 1 {
				t.Fatalf("durable incomplete abort=%s/%v/%d/%d", status, intent.Queued, reservations, intents)
			}
			if fault == "activity" {
				auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
				auditOSSExpireMultipartLease(t, f, ticket.ObjectKey)
			} else {
				f.store.failAbort.Store(false)
			}
			restarted := NewOSSMultipartCleanupWorker(f.server.cfg, f.db)
			restarted.drain(f.ctx)
			auditOSSAssertAbortFinal(t, f, ticket, intent.ID, 2)
			auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", auditOSSAbortRequest(ticket), 200)
			auditOSSAssertAbortFinal(t, f, ticket, intent.ID, 2)
		})
	}
}

func TestAuditCMP003OSSMultipartAbortRestartsRealActivityConsumerAndRewardsOnceIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	if _, err := f.db.Exec(f.ctx, `update task_definitions set status='disabled'`); err != nil {
		t.Fatal(err)
	}
	var task int64
	if err := f.db.QueryRow(f.ctx, `insert into task_definitions(code,name,condition,rewards,status) values('owned_oss_abort','Owned upload lifecycle','{"action":"upload","objectType":"file","metric":"count","target":2}'::jsonb,'{"experience":25,"currencies":{"diamond":3}}'::jsonb,'active') returning id`).Scan(&task); err != nil {
		t.Fatal(err)
	}
	monitor := auditFixActivityMonitor(t, f.test013Fixture)
	ticket := decodeTEST022Data[test037Ticket](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", ossDirectUploadRequest{OriginalName: "owned-reward.nbt", ContentType: "application/octet-stream", SizeBytes: ossMultipartThreshold, SHA256: strings.Repeat("c", 64), Category: "test037", Source: "blueprint"}, 200))
	counts := func() [7]int64 {
		var result [7]int64
		err := f.db.QueryRow(f.ctx, `select (select count(*) from user_activity_events where user_id=$1 and action_id=$3 and object_type_id=$4),coalesce((select sum(progress) from user_task_progress where user_id=$1 and task_id=$2),0),(select count(*) from user_task_progress where user_id=$1 and task_id=$2 and rewarded_at is not null),coalesce((select experience from user_experience where user_id=$1),0),coalesce((select b.balance from user_currency_balances b join currencies c on c.id=b.currency_id where b.user_id=$1 and c.code='diamond'),0),(select count(*) from currency_transactions where user_id=$1),(select count(*) from activity_event_outbox where user_id=$1)`, f.userIDs[f.editor], task, activity.ActionUpload, activity.ObjectFile).Scan(&result[0], &result[1], &result[2], &result[3], &result[4], &result[5], &result[6])
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	auditFixWaitActivity(t, func() bool { return counts() == [7]int64{1, 1, 0, 0, 0, 0, 0} })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := monitor.Close(ctx); err != nil {
		t.Fatal(err)
	}
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
	auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", auditOSSAbortRequest(ticket), 500)
	if got := counts(); got != [7]int64{1, 1, 0, 0, 0, 0, 0} {
		t.Fatalf("failed finalization changed rewards: %v", got)
	}
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
	auditOSSExpireMultipartLease(t, f, ticket.ObjectKey)
	NewOSSMultipartCleanupWorker(f.server.cfg, f.db).drain(f.ctx)
	if got := counts(); got != [7]int64{1, 1, 0, 0, 0, 0, 1} {
		t.Fatalf("recovery lost/delegated original outbox intent: %v", got)
	}
	auditFixActivityMonitor(t, f.test013Fixture)
	auditFixWaitActivity(t, func() bool { return counts() == [7]int64{2, 2, 1, 25, 3, 1, 0} })
	auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", auditOSSAbortRequest(ticket), 200)
	NewOSSMultipartCleanupWorker(f.server.cfg, f.db).drain(f.ctx)
	if got := counts(); got != [7]int64{2, 2, 1, 25, 3, 1, 0} {
		t.Fatalf("repeat duplicated original activity or reward: %v", got)
	}
}

func TestAuditCMP003OSSMultipartExpiredLeaseWithoutReplacementCannotCommitIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	ticket := decodeTEST022Data[test037Ticket](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", ossDirectUploadRequest{OriginalName: "owned-expired.nbt", ContentType: "application/octet-stream", SizeBytes: ossMultipartThreshold, SHA256: strings.Repeat("e", 64), Category: "test037", Source: "blueprint"}, 200))
	handler := f.provider.Config.Handler
	reached, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var first atomic.Bool
	f.provider.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Query().Get("uploadId") == ticket.Multipart.UploadID && first.CompareAndSwap(false, true) {
			handler.ServeHTTP(w, r)
			close(reached)
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		handler.ServeHTTP(w, r)
	})
	response := auditCompletionAsyncJSON(f.test013Fixture, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", auditOSSAbortRequest(ticket))
	select {
	case <-reached:
	case <-time.After(10 * time.Second):
		t.Fatal("actual post-provider expiry barrier not reached")
	}
	var leaseToken string
	if err := f.db.QueryRow(f.ctx, `select locked_by from oss_multipart_sessions where object_key=$1`, ticket.ObjectKey).Scan(&leaseToken); err != nil {
		t.Fatal(err)
	}
	auditOSSExpireMultipartLease(t, f, ticket.ObjectKey)
	unblock()
	result := <-response
	if result.err != nil || result.status != 500 || !strings.Contains(string(result.raw), "保存 OSS 分片取消状态失败") {
		t.Fatalf("expired unchanged-token handler committed status=%d err=%v; expected specific lease failure500", result.status, result.err)
	}
	status, intent, reservations, intents := auditOSSReadAbortIntent(t, f, ticket.ObjectKey)
	if status != "aborting" || intent.Queued || reservations != 1 || intents != 1 {
		t.Fatalf("expired attempt changed business facts=%s/%v/%d/%d", status, intent.Queued, reservations, intents)
	}
	var afterToken string
	if err := f.db.QueryRow(f.ctx, `select locked_by from oss_multipart_sessions where object_key=$1`, ticket.ObjectKey).Scan(&afterToken); err != nil || leaseToken == "" || afterToken != leaseToken {
		t.Fatalf("expiry-only probe unexpectedly replaced token: same=%v err=%v", afterToken == leaseToken, err)
	}
	NewOSSMultipartCleanupWorker(f.server.cfg, f.db).drain(f.ctx)
	auditOSSAssertAbortFinal(t, f, ticket, intent.ID, 2)
}

func TestAuditCMP003OSSMultipartExpiredLeaseCannotCompleteOrPersistLateFailureIntegration(t *testing.T) {
	for _, path := range []string{"complete_success", "complete_failure", "abort_failure", "cleanup_failure"} {
		t.Run(path, func(t *testing.T) {
			f := newTEST037Fixture(t)
			ticket := decodeTEST022Data[test037Ticket](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", ossDirectUploadRequest{OriginalName: "owned-expired-stage.zip", ContentType: "application/octet-stream", SizeBytes: ossMultipartThreshold, SHA256: strings.Repeat("f", 64), Category: "test037", Source: "test037-expired"}, 200))
			raw, err := json.Marshal(ticket.completion())
			if err != nil {
				t.Fatal(err)
			}
			var request ossCompleteUploadRequest
			if err = json.Unmarshal(raw, &request); err != nil {
				t.Fatal(err)
			}
			request.MultipartUploadID = ticket.Multipart.UploadID
			action := "complete"
			if strings.Contains(path, "abort") || path == "cleanup_failure" {
				action = "abort"
			}
			settlement, err := f.server.beginOSSMultipartSettlement(f.ctx, f.userIDs[f.editor], f.cfg, request, action)
			if err != nil {
				t.Fatal(err)
			}
			tag, err := f.db.Exec(f.ctx, `update oss_multipart_sessions set lease_expires_at=clock_timestamp()-interval '1 second' where id=$1 and locked_by=$2`, settlement.ID, settlement.Token)
			if err != nil || tag.RowsAffected() != 1 {
				t.Fatalf("expiry-only stage affected=%d err=%v", tag.RowsAffected(), err)
			}
			before := auditOSSAllFacts(t, f)
			if path == "cleanup_failure" {
				_, err = NewOSSMultipartCleanupWorker(f.server.cfg, f.db).recordFailure(f.ctx, ossMultipartCleanupJob{ID: settlement.ID, LockToken: settlement.Token, Attempts: 1, MaxAttempts: 12}, errors.New("owned provider transient failure"))
			} else {
				var providerErr error
				if path != "complete_success" {
					providerErr = errors.New("owned provider transient failure")
				}
				err = f.server.finishOSSMultipartSettlement(f.ctx, settlement, action, providerErr)
			}
			if !errors.Is(err, errOSSMultipartSessionLeaseLost) {
				t.Fatalf("expired %s returned unrelated result: %v", path, err)
			}
			if after := auditOSSAllFacts(t, f); !reflect.DeepEqual(before, after) {
				t.Fatalf("expired %s changed state, quota, audit or intent", path)
			}
		})
	}
}
