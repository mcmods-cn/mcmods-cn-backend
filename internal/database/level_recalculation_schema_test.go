package database

import (
	"strings"
	"testing"
)

func TestLevelRecalculationUsesGeneration133DurableVersionedJobs(t *testing.T) {
	t.Parallel()
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(communitySchemaStatements(), "\n"))
	for _, required := range []string{
		"version bigint not null default 1",
		"create table if not exists level_recalculation_jobs",
		"config_version bigint not null unique",
		"cursor_user_id bigint not null default 0",
		"processed_count bigint not null default 0",
		"locked_by text not null default ''",
		"lease_expires_at timestamptz",
		"max_attempts integer not null default 8",
		"check(status in ('queued','processing','completed','superseded','dead'))",
		"idx_level_recalculation_jobs_ready",
		"idx_level_recalculation_jobs_lease",
		"idx_level_recalculation_jobs_active",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("level recalculation schema is missing %q", required)
		}
	}
}
