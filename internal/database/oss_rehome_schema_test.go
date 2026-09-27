package database

import (
	"strings"
	"testing"
)

func TestOSSRehomeJobsHaveDurableGenerationLeaseAndReplayLifecycle(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("unexpected schema generation %d", schemaGeneration)
	}
	definition := strings.ToLower(strings.Join(schemaInstallationStatements(), "\n"))
	for _, required := range []string{
		"create table oss_rehome_jobs",
		"mod_id bigint not null unique references mods(id) on delete cascade",
		"generation bigint not null default 1",
		"claimed_generation bigint",
		"max_attempts integer not null default 8",
		"locked_by text not null default ''",
		"lease_expires_at timestamptz",
		"replay_count integer not null default 0",
		"last_replayed_by bigint references users(id) on delete set null",
		"check(status in ('queued','processing','completed','dead'))",
		"idx_oss_rehome_jobs_ready",
		"on oss_rehome_jobs(next_attempt_at,id) where status='queued' and attempts<max_attempts",
		"idx_oss_rehome_jobs_recovery",
		"on oss_rehome_jobs(lease_expires_at,id) where status='processing' and attempts<max_attempts",
		"idx_oss_rehome_jobs_exhausted",
		"on oss_rehome_jobs(updated_at,id) where status in ('queued','processing') and attempts>=max_attempts",
		"idx_oss_rehome_jobs_dead",
		"on oss_rehome_jobs(dead_at desc,id desc) where status='dead'",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("OSS rehome schema is missing %q", required)
		}
	}
}
