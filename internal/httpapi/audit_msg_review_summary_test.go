package httpapi

import (
	"net/http"
	"testing"
)

// These assertions cover the actual summary, not only item.Read. A controlled
// PostgreSQL commit barrier holds the real NotificationWorker after its write.
func TestAuditMsgReviewLateCommitSummaryMatchesVisibleRevisionIntegration(t *testing.T) {
	for _, aggregate := range []bool{false, true} {
		name := "insert"
		if aggregate {
			name = "aggregate"
		}
		t.Run(name, func(t *testing.T) {
			f := newTEST022Fixture(t, false)
			worker := auditNotifyWorker(f)
			user := f.userIDs[f.editor]
			event := notificationEvent{Action: "direct", Kind: "review", RecipientID: user, Title: "Audit summary late", Body: "Late commit"}
			if aggregate {
				event = notificationEvent{Action: "comment_watch", RecipientID: user, ActorID: f.userIDs[f.otherEditor], Title: "Audit summary watch", Body: "Visible first reply", Data: map[string]any{"watchId": "audit-msg-review-watch", "replyCount": 1}}
				if err := auditDeliver(worker, f.ctx, "audit-msg-summary-first", event); err != nil {
					t.Fatal(err)
				}
				event.Body = "Late aggregate commit"
			}
			condition, operation, key := "new.title='Audit summary late'", "insert", int64(92233401)
			if aggregate {
				condition, operation, key = "new.body='Late aggregate commit'", "update", 92233402
			}
			release := auditNotificationBarrier(t, f, condition, operation, key)
			done := make(chan error, 1)
			go func() { done <- auditDeliver(worker, f.ctx, "audit-msg-summary-late", event) }()
			auditWaitNotificationBlocked(t, f.db, f.ctx, key)
			// Establish a cached zero at the read boundary while the new revision is
			// still uncommitted; successful cache invalidation alone is insufficient.
			auditReadAll(t, f, f.editor)
			readSummary := func(token string) int64 {
				t.Helper()
				raw := f.requireAt(t, 0, token, http.MethodGet, "/api/v1/me/unread-summary", nil, 200)
				value := decodeTEST022Data[test021UnreadSummary](t, raw)
				if value.Messages != 0 || value.Total != value.Notifications {
					t.Fatalf("inconsistent summary: %+v", value)
				}
				return value.Notifications
			}
			if count := readSummary(f.editor); count != 0 {
				t.Fatalf("before late commit unread=%d", count)
			}
			release()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			items := auditNotificationItems(t, f, f.editor)
			if len(items) != 1 || items[0].Read {
				t.Fatalf("late revision missing/unread flag wrong: %+v", items)
			}
			if count := readSummary(f.editor); count != 1 {
				t.Fatalf("late revision summary=%d want1", count)
			}
			if count := readSummary(f.otherEditor); count != 0 {
				t.Fatalf("cross-user summary changed=%d", count)
			}
			if err := auditDeliver(worker, f.ctx, "audit-msg-summary-late", event); err != nil {
				t.Fatal(err)
			}
			if count := readSummary(f.editor); count != 1 {
				t.Fatalf("duplicate delivery changed summary=%d", count)
			}
			path := "/api/v1/notifications/" + items[0].ID + "/read"
			for round := 0; round < 2; round++ {
				f.requireAt(t, 0, f.editor, http.MethodPost, path, nil, 200)
				if count := readSummary(f.editor); count != 0 {
					t.Fatalf("read round%d unread=%d", round, count)
				}
			}
			auditReadAll(t, f, f.editor)
			if count := readSummary(f.editor); count != 0 {
				t.Fatalf("read-all after explicit read=%d", count)
			}
			t.Logf("late %s: actual item unread=true and HTTP summary1; duplicate1; explicit reads0; user isolation0", name)
		})
	}
}
