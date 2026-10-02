package database

import (
	"strings"
	"testing"
)

func TestOSSDeletionOutboxHasBoundedDeadAndReplayLifecycle(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("unexpected schema generation %d", schemaGeneration)
	}
	definition := strings.ToLower(strings.Join(schemaInstallationStatements(), "\n"))
	for _, required := range []string{
		"max_attempts integer not null default 12",
		"locked_by text not null default ''",
		"failure_class text not null default ''",
		"dead_at timestamptz",
		"replay_count integer not null default 0",
		"last_replayed_at timestamptz",
		"last_replayed_by bigint references users(id) on delete set null",
		"check(status in ('pending','processing','completed','dead'))",
		"check(max_attempts between 1 and 100)",
		"idx_oss_object_deletion_outbox_dead",
		"where status='dead'",
		"idx_oss_object_deletion_outbox_exhausted",
		"where status in ('pending','processing') and attempts>=max_attempts",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("OSS deletion schema is missing %q", required)
		}
	}
}
