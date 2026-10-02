package database

import (
	"strings"
	"testing"
)

func TestDirectMessagePagesHaveGeneration125Schema(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(schemaInstallationStatements(), "\n"))
	for _, required := range []string{
		"last_message_id bigint",
		"direct_conversations_last_message_id_fkey",
		"idx_direct_conversations_low_page",
		"(user_low_id, updated_at desc, id desc)",
		"idx_direct_conversations_high_page",
		"(user_high_id, updated_at desc, id desc)",
		"idx_direct_messages_conversation_id",
		"(conversation_id, id desc)",
		"idx_direct_messages_unread_conversation",
		"(recipient_id, conversation_id, id) where read_at is null",
		"create table if not exists direct_conversation_unread_counts",
		"unread_count bigint not null default 0",
		"create or replace function sync_direct_conversation_unread_count()",
		"create trigger trg_direct_messages_unread_count",
	} {
		if !strings.Contains(schema, required) {
			t.Errorf("direct-message schema is missing %q", required)
		}
	}
	for _, obsolete := range []string{
		"idx_direct_conversations_updated_at",
		"idx_direct_messages_conversation_created_at",
	} {
		if strings.Contains(schema, obsolete) {
			t.Errorf("direct-message schema retained obsolete index %q", obsolete)
		}
	}
}
