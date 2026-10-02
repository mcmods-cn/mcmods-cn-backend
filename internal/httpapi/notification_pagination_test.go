package httpapi

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNotificationCursorIsStrictAndBoundToUserKindAndLimit(t *testing.T) {
	for name, values := range map[string]url.Values{
		"offset":       {"offset": {"1"}},
		"page":         {"page": {"2"}},
		"unknown kind": {"kind": {"unknown"}},
		"zero limit":   {"limit": {"0"}},
		"large limit":  {"limit": {"101"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseNotificationPageRequest(values, 42); err == nil {
				t.Fatal("invalid notification page request was accepted")
			}
		})
	}
	first, err := parseNotificationPageRequest(url.Values{"kind": {"review"}, "limit": {"25"}}, 42)
	if err != nil {
		t.Fatal(err)
	}
	cursor := encodeNotificationPageCursor(notificationPageCursor{
		Version: notificationCursorVersion, Scope: first.Scope, UpdatedAt: time.Now().UTC(), ID: 99,
	})
	for _, test := range []struct {
		name   string
		userID int64
		values url.Values
	}{
		{"other user", 43, url.Values{"kind": {"review"}, "limit": {"25"}, "cursor": {cursor}}},
		{"other kind", 42, url.Values{"kind": {"system"}, "limit": {"25"}, "cursor": {cursor}}},
		{"other limit", 42, url.Values{"kind": {"review"}, "limit": {"26"}, "cursor": {cursor}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, parseErr := parseNotificationPageRequest(test.values, test.userID); parseErr == nil {
				t.Fatal("cross-scope notification cursor was accepted")
			}
		})
	}
	unknownField := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"s":"` + first.Scope + `","updatedAt":"2026-01-01T00:00:00Z","id":99,"extra":true}`))
	if _, err = parseNotificationPageRequest(url.Values{"kind": {"review"}, "limit": {"25"}, "cursor": {unknownField}}, 42); err == nil {
		t.Fatal("notification cursor with an unknown field was accepted")
	}
}

func TestNotificationPageSQLMergesBoundedDirectAndBroadcastKeysets(t *testing.T) {
	request, err := parseNotificationPageRequest(url.Values{"kind": {"review"}, "limit": {"25"}}, 42)
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor = &notificationPageCursor{UpdatedAt: time.Now().UTC(), ID: 99}
	query, args := notificationPageSQL(42, request)
	for _, required := range []string{
		"where n.recipient_id=$1 and n.kind=$2 and (n.updated_at,n.id)<($3,$4)",
		"where n.recipient_id is null and n.kind=$2 and (n.updated_at,n.id)<($3,$4)",
		"union all", "limit $5", "notification_read_watermarks", "order by n.updated_at desc,n.id desc",
		"order by actor.id desc limit 3",
	} {
		if !strings.Contains(query, required) {
			t.Errorf("notification page query is missing %q: %s", required, query)
		}
	}
	if len(args) != 5 || args[0] != int64(42) || args[1] != "review" || args[4] != 26 {
		t.Fatalf("unexpected notification page args %v", args)
	}
	if strings.Contains(strings.ToLower(markAllNotificationsReadSQL), "notification_receipts") ||
		!strings.Contains(markAllNotificationsReadSQL, "select max(id) from notifications") {
		t.Fatal("read-all is not a single watermark upsert")
	}
	for _, required := range []string{"notificationUnreadPredicateSQL", "notification_read_watermarks", "where notification_receipts.read_at is null"} {
		if required == "notificationUnreadPredicateSQL" {
			if !strings.Contains(markNotificationReadSQL, "receipt.notification_id is not null") {
				t.Fatal("single-read SQL does not use the logical unread predicate")
			}
			continue
		}
		if !strings.Contains(markNotificationReadSQL, required) {
			t.Errorf("single-read SQL is missing %q", required)
		}
	}
}
