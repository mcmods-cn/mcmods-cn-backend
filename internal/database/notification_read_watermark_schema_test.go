package database

import (
	"strings"
	"testing"
)

func TestNotificationPagesAndReadWatermarksHaveGeneration125Schema(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(schemaInstallationStatements(), "\n"))
	for _, required := range []string{
		"create table if not exists notification_read_watermarks",
		"max_notification_id bigint not null default 0",
		"primary key references users(id) on delete cascade",
		"idx_notifications_recipient_page",
		"(recipient_id, updated_at desc, id desc)",
		"idx_notifications_recipient_kind_page",
		"(recipient_id, kind, updated_at desc, id desc)",
		"idx_notifications_recipient_id",
		"(recipient_id, id)",
		"idx_notifications_broadcast_page",
		"(updated_at desc, id desc) where recipient_id is null",
		"idx_notifications_broadcast_kind_page",
		"(kind, updated_at desc, id desc) where recipient_id is null",
		"idx_notifications_broadcast_id",
		"(id) where recipient_id is null",
		"create table if not exists notification_broadcast_state",
		"live_count bigint not null default 0",
		"max_notification_id bigint not null default 0",
		"create or replace function sync_notification_broadcast_state()",
		"create trigger trg_notifications_broadcast_state",
	} {
		if !strings.Contains(schema, required) {
			t.Errorf("notification schema is missing %q", required)
		}
	}
}
