package httpapi

// Audit-only file added to a disposable copy; no production source changes.
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

func auditNotifyWorker(f test022Fixture) *NotificationWorker {
	return NewNotificationWorker(f.db, nil, f.servers[0].cache, config.SMTPConfig{}, "")
}
func auditDeliver(w *NotificationWorker, ctx context.Context, id string, e notificationEvent) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return w.handleEvent(queue.WithEventID(ctx, id), raw)
}
func auditNotificationItems(t *testing.T, f test022Fixture, token string) []notificationItem {
	t.Helper()
	raw := f.requireAt(t, 0, token, http.MethodGet, "/api/v1/notifications?limit=100", nil, 200)
	return decodeTEST022Data[test021NotificationPage](t, raw).Items
}
func auditReadAll(t *testing.T, f test022Fixture, token string) {
	t.Helper()
	raw := f.requireAt(t, 0, token, http.MethodPost, "/api/v1/notifications/read-all", nil, 200)
	t.Logf("read_all_response=%s", raw)
}

// The lock is exclusively inside the newly-created test database. A BEFORE/AFTER
// trigger here controls commit order, without disabling any baseline trigger.
func auditNotificationBarrier(t *testing.T, f test022Fixture, condition, operation string, key int64) func() {
	t.Helper()
	conn, err := f.db.Acquire(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(f.ctx, "select pg_advisory_lock($1)", key); err != nil {
		conn.Release()
		t.Fatal(err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := conn.Exec(cleanup, "select pg_advisory_unlock($1)", key)
			if err != nil {
				t.Error(err)
			}
			conn.Release()
		})
	}
	t.Cleanup(release)
	q := fmt.Sprintf("create function audit_notify_barrier() returns trigger language plpgsql as $$ begin if %s then perform pg_advisory_xact_lock(%d); end if; return new; end $$; create trigger audit_notify_barrier after %s on notifications for each row execute function audit_notify_barrier()", condition, key, operation)
	if _, err = f.db.Exec(f.ctx, q); err != nil {
		release()
		t.Fatal(err)
	}
	return release
}
func auditWaitNotificationBlocked(t *testing.T, pool *pgxpool.Pool, ctx context.Context, key int64) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var blocked int
		err := pool.QueryRow(ctx, "select count(*) from pg_locks where locktype='advisory' and objid=$1 and not granted and database=(select oid from pg_database where datname=current_database())", key).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("controlled worker did not reach transaction barrier")
}
func TestAuditNotificationConcurrentReadAndDuplicateDelivery(t *testing.T) {
	f := newTEST022Fixture(t, false)
	worker := auditNotifyWorker(f)
	user := f.userIDs[f.editor]
	event := notificationEvent{Action: "direct", Kind: "review", RecipientID: user, Title: "Audit concurrent exact once", Body: "One logical event"}
	errs := make(chan error, 6)
	for range 6 {
		go func() { errs <- auditDeliver(worker, f.ctx, "audit-concurrent-direct", event) }()
	}
	for range 6 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	items := auditNotificationItems(t, f, f.editor)
	if len(items) != 1 {
		t.Fatalf("duplicate events produced %d rows", len(items))
	}
	if len(auditNotificationItems(t, f, f.otherEditor)) != 0 {
		t.Fatal("recipient isolation failed")
	}
	// Six full mounted HTTP mutations overlap, including repeated reads of one object.
	type response struct {
		status int
		body   []byte
	}
	out := make(chan response, 6)
	for i := 0; i < 6; i++ {
		go func(all bool) {
			path := "/api/v1/notifications/" + items[0].ID + "/read"
			if all {
				path = "/api/v1/notifications/read-all"
			}
			request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.origins[0].URL+path, nil)
			if err != nil {
				out <- response{0, []byte(err.Error())}
				return
			}
			request.Header.Set("Authorization", "Bearer "+f.editor)
			res, err := f.origins[0].Client().Do(request)
			if err != nil {
				out <- response{0, []byte(err.Error())}
				return
			}
			body, err := io.ReadAll(io.LimitReader(res.Body, 65536))
			res.Body.Close()
			if err != nil {
				out <- response{0, []byte(err.Error())}
				return
			}
			out <- response{res.StatusCode, body}
		}(i%2 == 0)
	}
	for range 6 {
		r := <-out
		if r.status != 200 {
			t.Fatalf("read mutation=%d %s", r.status, r.body)
		}
	}
	items = auditNotificationItems(t, f, f.editor)
	if len(items) != 1 || !items[0].Read {
		t.Fatalf("post-read items=%+v", items)
	}
	var rows, receipts int
	if err := f.db.QueryRow(f.ctx, "select (select count(*) from notifications where source_event_id='audit-concurrent-direct'),(select count(*) from notification_receipts where user_id=$1)", user).Scan(&rows, &receipts); err != nil {
		t.Fatal(err)
	}
	raw := f.requireAt(t, 0, f.editor, http.MethodGet, "/api/v1/me/unread-summary", nil, 200)
	summary := decodeTEST022Data[test021UnreadSummary](t, raw)
	t.Logf("logical_events=1 notifications=%d receipts=%d unread=%s", rows, receipts, raw)
	if rows != 1 || receipts > 1 || summary.Notifications != 0 {
		t.Fatal("duplicate/read idempotency mismatch")
	}
}
func TestAuditNotificationLateInsertRemainsUnreadAfterWatermark(t *testing.T) {
	f := newTEST022Fixture(t, false)
	worker := auditNotifyWorker(f)
	user := f.userIDs[f.editor]
	release := auditNotificationBarrier(t, f, "new.title='Audit delayed insert'", "insert", 92233017)
	done := make(chan error, 1)
	go func() {
		done <- auditDeliver(worker, f.ctx, "audit-delayed-insert", notificationEvent{Action: "direct", Kind: "review", RecipientID: user, Title: "Audit delayed insert", Body: "Invisible until after read-all completed"})
	}()
	auditWaitNotificationBlocked(t, f.db, f.ctx, 92233017)
	if err := auditDeliver(worker, f.ctx, "audit-visible-later-id", notificationEvent{Action: "direct", Kind: "review", RecipientID: user, Title: "Audit visible later ID", Body: "Already visible before read-all"}); err != nil {
		t.Fatal(err)
	}
	before := auditNotificationItems(t, f, f.editor)
	if len(before) != 1 || before[0].Title != "Audit visible later ID" {
		t.Fatalf("snapshot before read-all=%+v", before)
	}
	auditReadAll(t, f, f.editor)
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	after := auditNotificationItems(t, f, f.editor)
	var late *notificationItem
	for i := range after {
		if after[i].Title == "Audit delayed insert" {
			late = &after[i]
		}
	}
	var facts string
	if err := f.db.QueryRow(f.ctx, "select json_build_object('rows',(select json_agg(json_build_object('id',id,'title',title,'updated_at',updated_at) order by id) from notifications),'watermark',(select row_to_json(w) from notification_read_watermarks w where user_id=$1))::text", user).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	raw := f.requireAt(t, 0, f.editor, http.MethodGet, "/api/v1/me/unread-summary", nil, 200)
	t.Logf("before=%+v after=%+v db=%s unread=%s", before, after, facts, raw)
	if len(after) != 2 || late == nil {
		t.Fatal("late insertion missing")
	}
	if late.Read {
		t.Error("late committed notification was never visible to read-all but returned read=true")
	}
}
func TestAuditNotificationLateAggregationRemainsUnreadAfterWatermark(t *testing.T) {
	f := newTEST022Fixture(t, false)
	worker := auditNotifyWorker(f)
	user := f.userIDs[f.editor]
	first := notificationEvent{Action: "comment_watch", RecipientID: user, ActorID: f.userIDs[f.otherEditor], Title: "Audit existing watch", Body: "First visible reply", Data: map[string]any{"watchId": "audit-owned-watch", "replyCount": 1}}
	if err := auditDeliver(worker, f.ctx, "audit-watch-first", first); err != nil {
		t.Fatal(err)
	}
	before := auditNotificationItems(t, f, f.editor)
	if len(before) != 1 {
		t.Fatalf("before=%+v", before)
	}
	release := auditNotificationBarrier(t, f, "new.body='Late reply after read-all'", "update", 92233018)
	second := first
	second.Body = "Late reply after read-all"
	done := make(chan error, 1)
	go func() { done <- auditDeliver(worker, f.ctx, "audit-watch-late", second) }()
	auditWaitNotificationBlocked(t, f.db, f.ctx, 92233018)
	visible := auditNotificationItems(t, f, f.editor)
	if len(visible) != 1 || visible[0].Body != "First visible reply" {
		t.Fatalf("uncommitted update visible: %+v", visible)
	}
	auditReadAll(t, f, f.editor)
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	after := auditNotificationItems(t, f, f.editor)
	raw := f.requireAt(t, 0, f.editor, http.MethodGet, "/api/v1/me/unread-summary", nil, 200)
	t.Logf("before=%+v after=%+v unread=%s", visible, after, raw)
	if len(after) != 1 || after[0].Body != "Late reply after read-all" {
		t.Fatal("late source update missing")
	}
	if after[0].Read {
		t.Error("new reply committed after read-all was incorrectly returned as read")
	}
}
