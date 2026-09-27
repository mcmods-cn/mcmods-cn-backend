package database

import (
	"strings"
	"testing"
)

func TestBUG032ProjectFilePublicationUsesGeneration145SafetyState(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("schema generation=%d want 167 for scan-gated project-file publication", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(projectFileSchemaStatements(), "\n"))
	for _, required := range []string{
		"status text not null default 'processing'",
		"publication_generation integer not null default 0",
		"check (status in ('processing','active','rejected','deleted'))",
		"ensure_project_file_publication_safe",
		"trg_project_files_publication_safe",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("project-file schema lacks BUG032 invariant %q", required)
		}
	}
}
