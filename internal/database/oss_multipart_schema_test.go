package database

import (
	"strings"
	"testing"
)

func TestOSSMultipartSessionsHavePersistentBoundedCleanupLifecycle(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("unexpected schema generation %d", schemaGeneration)
	}
	definition := strings.ToLower(strings.Join(schemaInstallationStatements(), "\n"))
	for _, required := range []string{
		"create table oss_multipart_sessions",
		"owner_id bigint references users(id) on delete set null",
		"upload_id text not null",
		"expires_at timestamptz not null",
		"check(status in ('active','completing','cleanup_pending','aborting','completed','aborted','dead'))",
		"max_attempts integer not null default 12",
		"idx_oss_multipart_sessions_expired",
		"idx_oss_multipart_sessions_cleanup",
		"idx_oss_multipart_sessions_lease",
		"idx_oss_multipart_sessions_exhausted",
		"idx_oss_multipart_sessions_dead",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("OSS multipart schema is missing %q", required)
		}
	}
}
