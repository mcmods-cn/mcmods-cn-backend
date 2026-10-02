package database

import (
	"strings"
	"testing"
)

func TestSearchRebuildProgressUsesGeneration125BoundedState(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(searchSchemaStatements(), "\n"))
	for _, required := range []string{
		"create table search_index_rebuild_progress",
		"collection_kind text primary key",
		"last_document_id bigint",
		"indexed_document_count bigint",
		"indexed_byte_count bigint",
		"batch_count bigint",
		"status text",
	} {
		if !strings.Contains(schema, required) {
			t.Errorf("search rebuild progress schema is missing %q", required)
		}
	}
}
