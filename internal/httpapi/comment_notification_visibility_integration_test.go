package httpapi

import (
	"bytes"
	"net/http"
	"testing"

	"mcmods-cn-backend/internal/queue"
)

func TestCommentNotificationsUseCurrentRecipientVisibilityIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	root := f.comment(t, f.otherEditor, f.otherModCode, "notice-root", "Ordinary forum thread", "")
	directParent := f.comment(t, f.editor, f.otherModCode, "notice-direct-parent", "A normal reply by the first reader", root.ID)
	f.require(t, f.editor, http.MethodPut, "/api/v1/comments/"+root.ID+"/watch", nil, http.StatusOK)
	worker := NewNotificationWorker(f.db, nil, f.server.cache, f.server.cfg.SMTP, f.server.cfg.SettingsEncryptionKey)
	const noticePath = "/api/v1/notifications"

	publicReply := f.comment(t, f.otherEditor, f.otherModCode, "notice-public-reply", "A normal new comment", directParent.ID)
	deliverTestCommentNotices(t, f, worker, publicReply.ID, 1)
	publicWatch := f.comment(t, f.otherEditor, f.otherModCode, "notice-public-watch", "A normal watched update", root.ID)
	deliverTestCommentNotices(t, f, worker, publicWatch.ID, 1)
	visible := decodeTEST022Data[struct{ Items []notificationItem }](t, f.require(t, f.editor, http.MethodGet, noticePath, nil, http.StatusOK))
	if len(visible.Items) != 2 {
		t.Fatalf("visible reply and watch notifications=%d, want both", len(visible.Items))
	}
	for _, item := range visible.Items {
		origin := publicReply
		if item.Kind == "comment_watch_reply" {
			origin = publicWatch
		}
		if item.Data["url"] != "/comments/"+origin.ID || item.Body == origin.Body || item.Data["targetLabel"] != nil {
			t.Error("a visible comment notification should contain a neutral reminder and its permission-checked thread link")
		}
	}

	hideTestCommentLogTarget(t, f, "mod", f.otherModCode)
	f.require(t, f.editor, http.MethodGet, "/api/v1/comments/"+root.ID+"/thread", nil, http.StatusNotFound)
	f.require(t, f.otherEditor, http.MethodGet, "/api/v1/comments/"+root.ID+"/thread", nil, http.StatusOK)
	var dropped []commentResponse
	for _, scenario := range []struct{ key, parent, body string }{
		{"notice-restricted-direct", directParent.ID, "A new reply in the restricted thread"},
		{"notice-restricted-watch", root.ID, "Another update in the restricted thread"},
	} {
		t.Run(scenario.key, func(t *testing.T) {
			reply := f.comment(t, f.otherEditor, f.otherModCode, scenario.key, scenario.body, scenario.parent)
			dropped = append(dropped, reply)
			deliverTestCommentNotices(t, f, worker, reply.ID, 1)
			listed := f.require(t, f.editor, http.MethodGet, noticePath, nil, http.StatusOK)
			if bytes.Contains(listed, []byte(reply.ID)) || bytes.Contains(listed, []byte(reply.Body)) {
				t.Error("a newly restricted thread produced a notification for a former viewer")
			}
		})
	}
	// Restore the fixture's access to verify that replay cannot create a second
	// outcome for a delivery already completed without a notification.
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='approved' where id=$1`, f.otherModID); err != nil {
		t.Fatal(err)
	}
	for _, reply := range dropped {
		deliverTestCommentNotices(t, f, worker, reply.ID, 1)
	}
	afterReplay := decodeTEST022Data[struct{ Items []notificationItem }](t, f.require(t, f.editor, http.MethodGet, noticePath, nil, http.StatusOK))
	var firstWatch notificationItem
	for _, item := range afterReplay.Items {
		if item.Kind == "comment_watch_reply" {
			firstWatch = item
		}
	}
	if len(afterReplay.Items) != 2 || firstWatch.Data["commentId"] != publicWatch.ID {
		t.Fatal("replaying a finished inaccessible event changed the notification history")
	}
}

func deliverTestCommentNotices(t *testing.T, f test038Fixture, worker *NotificationWorker, commentID string, want int) {
	t.Helper()
	rows, err := f.db.Query(f.ctx, `select event_id,payload from nats_outbox
		where payload->'data'->>'commentId'=$1 and event_type in ('notification.direct','notification.comment_watch') order by id`, commentID)
	if err != nil {
		t.Fatal(err)
	}
	type message struct {
		id   string
		body []byte
	}
	var messages []message
	for rows.Next() {
		var item message
		if err = rows.Scan(&item.id, &item.body); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		messages = append(messages, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil || len(messages) != want {
		t.Fatalf("durable notification intents=%d want=%d err=%v", len(messages), want, err)
	}
	for _, item := range messages {
		if err := worker.handleEvent(queue.WithEventID(f.ctx, item.id), item.body); err != nil {
			t.Fatal(err)
		}
	}
}
