package database

import (
	"strings"
	"testing"
)

func TestContentHistoryPaginationIndexesGeneration133(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168", schemaGeneration)
	}
	statements := strings.Join(schemaInstallationStatements(), "\n")
	for _, required := range []string{
		"idx_content_revisions_history",
		"content_revisions(aggregate_type,aggregate_key,created_at desc,public_id desc)",
		"idx_resource_import_snapshots_history",
		"resource_import_snapshots(resource_id,created_at desc,revision_id desc)",
	} {
		if !strings.Contains(statements, required) {
			t.Fatalf("generation 133 schema is missing %q", required)
		}
	}
}
