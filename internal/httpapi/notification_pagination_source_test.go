package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestNotificationListsUseStablePagesAndReadAllUsesOneWatermark(t *testing.T) {
	handlerRaw, err := os.ReadFile("notification_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	handler := string(handlerRaw)
	combined := handler
	if paginationRaw, readErr := os.ReadFile("notification_pagination.go"); readErr == nil {
		combined += string(paginationRaw)
	}
	for _, required := range []string{
		"parseNotificationPageRequest", `"hasMore"`, `"nextCursor"`, "notification_read_watermarks",
		"notificationPageSQL", "writeBoundedCatalogJSON",
	} {
		if !strings.Contains(combined, required) {
			t.Errorf("notification pagination is missing %q", required)
		}
	}
	readAllStart := strings.Index(handler, "func (s *Server) markAllNotificationsRead")
	if readAllStart < 0 {
		t.Fatal("read-all handler not found")
	}
	readAllEnd := strings.Index(handler[readAllStart:], "func (s *Server) unreadSummary")
	if readAllEnd < 0 {
		t.Fatal("read-all handler boundary not found")
	}
	readAll := handler[readAllStart : readAllStart+readAllEnd]
	if strings.Contains(readAll, "insert into notification_receipts") || strings.Contains(readAll, "select id, $1, now() from notifications") {
		t.Fatal("read-all still materializes one receipt per historical notification")
	}

	schemaRaw, err := os.ReadFile("../database/migrations.go")
	if err != nil {
		t.Fatal(err)
	}
	schema := string(schemaRaw)
	for _, required := range []string{
		"create table if not exists notification_read_watermarks",
		"idx_notifications_recipient_page", "idx_notifications_recipient_kind_page",
		"idx_notifications_broadcast_page", "idx_notifications_broadcast_kind_page",
	} {
		if !strings.Contains(schema, required) {
			t.Errorf("notification schema is missing %s", required)
		}
	}
}
