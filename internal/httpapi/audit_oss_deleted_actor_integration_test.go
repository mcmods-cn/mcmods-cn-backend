package httpapi

import (
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuditOSSMultipartControlledDeletedActorCleansWithoutInventedRewardIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	owner, _, token := createTEST044User(t, f.ctx, f.db, f.server.cfg, "oss-cancel-deleted-owner", "UTC")
	grantTEST044Permissions(t, f.ctx, f.db, owner, "user.file.single_limit.64", "user.file.daily_limit.256", "user.file.total_limit.256")
	f.userIDs[token] = owner
	f.editor = token
	ticket := decodeTEST022Data[test037Ticket](t, auditOSSRequire(t, f, token, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", ossDirectUploadRequest{OriginalName: "owned-deleted-actor.zip", ContentType: "application/octet-stream", SizeBytes: ossMultipartThreshold, SHA256: strings.Repeat("a", 64), Category: "test037", Source: "test037-deleted-actor"}, 200))
	handler := f.provider.Config.Handler
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
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
	response := auditCompletionAsyncJSON(f.test013Fixture, token, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", auditOSSAbortRequest(ticket))
	select {
	case <-reached:
	case <-time.After(10 * time.Second):
		t.Fatal("ordinary owner's actual abort did not reach provider barrier")
	}
	_, original, _, _ := auditOSSReadAbortIntent(t, f, ticket.ObjectKey)
	// The product exposes no account-hard-delete journey. This separately tests
	// the actual FK-deletion recovery state on an owned copy, not normal user
	// reachability or a claim that an ordinary user can delete another account.
	if _, err := f.db.Exec(f.ctx, `delete from users where id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	auditOSSExpireMultipartLease(t, f, ticket.ObjectKey)
	NewOSSMultipartCleanupWorker(f.server.cfg, f.db).drain(f.ctx)
	status, intent, reservations, intents := auditOSSReadAbortIntent(t, f, ticket.ObjectKey)
	if status != "aborted" || intent.Queued || !intent.ActorDeleted || intent.ID != original.ID || intent.Event.UserID != owner || reservations != 0 || intents != 0 {
		t.Fatalf("deleted-actor recovery state=%s queued=%v actorDeleted=%v originalID=%v reservation/intents=%d/%d", status, intent.Queued, intent.ActorDeleted, intent.ID == original.ID, reservations, intents)
	}
	var sessionOwner *int64
	var rewards int
	if err := f.db.QueryRow(f.ctx, `select owner_id,(select count(*) from currency_transactions where user_id=$2)+(select count(*) from user_experience where user_id=$2) from oss_multipart_sessions where object_key=$1`, ticket.ObjectKey, owner).Scan(&sessionOwner, &rewards); err != nil || sessionOwner != nil || rewards != 0 {
		t.Fatalf("deleted actor acquired replacement owner or rewards: owner=%v rewards=%d err=%v", sessionOwner, rewards, err)
	}
	unblock()
	result := <-response
	if result.err != nil || result.status != 500 || !strings.Contains(string(result.raw), "保存 OSS 分片取消状态失败") {
		t.Fatalf("late deleted-owner request result=%d err=%v", result.status, result.err)
	}
	NewOSSMultipartCleanupWorker(f.server.cfg, f.db).drain(f.ctx)
	_, after, _, count := auditOSSReadAbortIntent(t, f, ticket.ObjectKey)
	if after.ID != original.ID || !after.ActorDeleted || after.Queued || count != 0 {
		t.Fatal("repeated cleanup invented a deleted-account event")
	}
}
