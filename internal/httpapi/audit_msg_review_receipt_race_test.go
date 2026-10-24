package httpapi

import (
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

func auditMsgReviewReceiptBarrier(t *testing.T, f test022Fixture, key int64) func() {
	t.Helper()
	connection, err := f.db.Acquire(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = connection.Exec(f.ctx, `select pg_advisory_lock($1)`, key); err != nil {
		connection.Release()
		t.Fatal(err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := connection.Exec(ctx, `select pg_advisory_unlock($1)`, key); err != nil {
				t.Error(err)
			}
			connection.Release()
		})
	}
	t.Cleanup(release)
	if _, err = f.db.Exec(f.ctx, `create table audit_msg_receipt_gate(revision_xid xid8 not null);
 insert into audit_msg_receipt_gate select revision_xid from notifications where kind='new_follower' order by id limit 1;
 create function audit_msg_receipt_before_insert() returns trigger language plpgsql as $$ begin if new.read_revision_xid=(select revision_xid from audit_msg_receipt_gate) then perform pg_advisory_xact_lock(92233405);end if;return new;end $$;
 create trigger audit_msg_receipt_before_insert before insert on notification_receipts for each row execute function audit_msg_receipt_before_insert()`); err != nil {
		t.Fatal(err)
	}
	return release
}

func TestAuditMsgReviewReadAllCoversRevisionAfterLateSingleReceiptIntegration(t *testing.T) {
	auditMsgReviewLateReceipt(t, false)
}
func TestAuditMsgReviewLateSingleReadCannotReplaceNewerRevisionReceiptIntegration(t *testing.T) {
	auditMsgReviewLateReceipt(t, true)
}
func auditMsgReviewLateReceipt(t *testing.T, newerSingleRead bool) {
	f := newTEST022Fixture(t, false)
	worker := auditNotifyWorker(f)
	user := f.userIDs[f.editor]
	first := notificationEvent{Action: "follow", RecipientID: user, ActorID: f.userIDs[f.otherEditor]}
	if err := auditDeliver(worker, f.ctx, "audit-msg-follower-first", first); err != nil {
		t.Fatal(err)
	}
	items := auditNotificationItems(t, f, f.editor)
	if len(items) != 1 {
		t.Fatalf("first follower=%+v", items)
	}
	workerRelease := auditNotificationBarrier(t, f, "new.kind='new_follower'", "update", 92233404)
	second := first
	second.ActorID = f.userIDs[f.reviewer]
	workerDone := make(chan error, 1)
	go func() { workerDone <- auditDeliver(worker, f.ctx, "audit-msg-follower-late", second) }()
	auditWaitNotificationBlocked(t, f.db, f.ctx, 92233404)
	receiptRelease := auditMsgReviewReceiptBarrier(t, f, 92233405)
	type readResult struct {
		status int
		err    error
	}
	readDone := make(chan readResult, 1)
	go func() {
		request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.origins[0].URL+"/api/v1/notifications/"+items[0].ID+"/read", nil)
		if err != nil {
			readDone <- readResult{err: err}
			return
		}
		request.Header.Set("Authorization", "Bearer "+f.editor)
		response, err := f.origins[0].Client().Do(request)
		if err != nil {
			readDone <- readResult{err: err}
			return
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		readDone <- readResult{status: response.StatusCode}
	}()
	// Receipt BEFORE INSERT proves the real HTTP statement already selected
	// the old visible revision; keep it held until the late Worker commits.
	auditWaitNotificationBlocked(t, f.db, f.ctx, 92233405)
	workerRelease()
	if err := <-workerDone; err != nil {
		t.Fatal(err)
	}
	var expectedRevision string
	if newerSingleRead {
		f.requireAt(t, 0, f.editor, http.MethodPost, "/api/v1/notifications/"+items[0].ID+"/read", nil, 200)
		if err := f.db.QueryRow(f.ctx, `select receipt.read_revision_xid::text from notification_receipts receipt join notifications notification on notification.id=receipt.notification_id where notification.public_id=$1 and receipt.user_id=$2`, items[0].ID, user).Scan(&expectedRevision); err != nil {
			t.Fatal(err)
		}
	}
	receiptRelease()
	result := <-readDone
	if result.err != nil || result.status != 200 {
		t.Fatalf("single read=%d/%v", result.status, result.err)
	}
	summary := func() int64 {
		t.Helper()
		raw := f.requireAt(t, 0, f.editor, http.MethodGet, "/api/v1/me/unread-summary", nil, 200)
		return decodeTEST022Data[test021UnreadSummary](t, raw).Notifications
	}
	if newerSingleRead {
		var storedRevision string
		if err := f.db.QueryRow(f.ctx, `select receipt.read_revision_xid::text from notification_receipts receipt join notifications notification on notification.id=receipt.notification_id where notification.public_id=$1 and receipt.user_id=$2`, items[0].ID, user).Scan(&storedRevision); err != nil {
			t.Fatal(err)
		}
		if storedRevision != expectedRevision {
			t.Errorf("late old read replaced receipt for already-observed new revision: stored=%s expected=%s", storedRevision, expectedRevision)
		}
		truth, err := loadUnreadTruth(f.ctx, f.db, unreadTruthInternalUsersSQL, []int64{user})
		if err != nil || len(truth) != 1 || truth[0].Summary.Notifications != 0 {
			t.Errorf("two single reads truth=%+v/%v", truth, err)
		}
		after := auditNotificationItems(t, f, f.editor)
		if len(after) != 1 || !after[0].Read {
			t.Errorf("new-revision single-read marker lost: %+v", after)
		}
		if count := summary(); count != 0 {
			t.Errorf("two single reads summary=%d", count)
		}
		return
	}
	if count := summary(); count != 1 {
		t.Fatalf("late revision should remain unread after old-revision receipt: %d", count)
	}
	// This request starts after both commits and therefore observes the new
	// revision. An older per-item receipt must not override this newer read-all.
	auditReadAll(t, f, f.editor)
	if count := summary(); count != 0 {
		t.Errorf("new read-all did not cover visible revision after stale receipt: %d", count)
	}
	after := auditNotificationItems(t, f, f.editor)
	if len(after) != 1 || !after[0].Read {
		t.Errorf("read-all item remains unread: %+v", after)
	}
}

func TestAuditMsgReviewReadAllPreservesExplicitNullUnreadPolicyIntegration(t *testing.T) {
	f := newTEST022Fixture(t, false)
	worker := auditNotifyWorker(f)
	user := f.userIDs[f.editor]
	for id, event := range map[string]notificationEvent{
		"audit-null-direct":    {Action: "direct", Kind: "review", RecipientID: user, Title: "Explicit unread direct", Body: "Synthetic retained marker"},
		"audit-null-broadcast": {Action: "system", Title: "Explicit unread broadcast", Body: "Synthetic retained marker"},
	} {
		if err := auditDeliver(worker, f.ctx, id, event); err != nil {
			t.Fatal(err)
		}
	}
	// These are valid persisted explicit-unread markers, not a claim that this
	// test exercised a public mark-unread UI or an abnormal database value.
	if _, err := f.db.Exec(f.ctx, `insert into notification_receipts(notification_id,user_id,read_at,read_revision_xid) select id,$1,null,revision_xid from notifications`, user); err != nil {
		t.Fatal(err)
	}
	summary := func(token string) int64 {
		t.Helper()
		raw := f.requireAt(t, 0, token, http.MethodGet, "/api/v1/me/unread-summary", nil, 200)
		return decodeTEST022Data[test021UnreadSummary](t, raw).Notifications
	}
	auditReadAll(t, f, f.editor)
	if count := summary(f.editor); count != 2 {
		t.Fatalf("read-all erased explicit null unread markers: %d", count)
	}
	items := auditNotificationItems(t, f, f.editor)
	if len(items) != 2 {
		t.Fatalf("items=%d", len(items))
	}
	for _, item := range items {
		if item.Read {
			t.Errorf("explicit unread item read: %+v", item)
		}
	}
	auditReadAll(t, f, f.otherEditor)
	if count := summary(f.otherEditor); count != 0 {
		t.Fatalf("other user read-all=%d", count)
	}
	if count := summary(f.editor); count != 2 {
		t.Fatalf("other user's watermarks changed explicit markers=%d", count)
	}
	for _, item := range items {
		f.requireAt(t, 0, f.editor, http.MethodPost, "/api/v1/notifications/"+item.ID+"/read", nil, 200)
	}
	if count := summary(f.editor); count != 0 {
		t.Fatalf("current explicit reads did not clear own markers=%d", count)
	}
}
