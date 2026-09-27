package database

import (
	"strings"
	"testing"
)

func TestGovernancePaginationUsesGeneration145Indexes(t *testing.T) {
	t.Parallel()
	if schemaGeneration != 167 {
		t.Fatalf("schema generation=%d want 167", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(governanceAutomationSchemaStatements(), "\n"))
	for _, required := range []string{
		"idx_reports_reporter_history",
		"on reports(reporter_id,created_at desc,id desc)",
		"idx_reports_queue",
		"on reports(status,created_at,id)",
		"idx_ban_records_public",
		"on ban_records(created_at desc,id desc)",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("governance page schema is missing %q", required)
		}
	}
}
