package database

import (
	"strings"
	"testing"
)

func TestCommentLogAttachmentJobsHaveDurableRetrySchema(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("schema generation = %d, want 167 for durable comment log attachment jobs", schemaGeneration)
	}
	definition := strings.ToLower(strings.Join(featureUpdateSchemaStatements(), "\n"))
	for _, required := range []string{
		"comment_log_attachment_jobs",
		"unique(comment_id,attachment_file_id)",
		"foreign key(comment_id,attachment_file_id)",
		"idx_comment_log_attachment_jobs_pending",
		"where status='queued'",
		"idx_comment_log_attachment_jobs_processing",
		"where status='processing'",
		"idx_comment_log_attachment_jobs_requester",
		"attempts integer not null default 0",
		"max_attempts integer not null default 5",
		"lease_expires_at timestamptz",
		"next_attempt_at timestamptz not null default now()",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("comment log attachment job schema is missing %q", required)
		}
	}
}
