package database

import (
	"os"
	"strings"
	"testing"
)

func TestBlueprintMaterialRevisionLookupHasGeneration126NamespaceIndex(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("schema generation = %d, want 167", schemaGeneration)
	}
	sourceBytes, err := os.ReadFile("migrations.go")
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ToLower(string(sourceBytes))
	for _, fragment := range []string{
		"create index idx_catalog_import_revisions_material_namespace",
		"on catalog_import_revisions(source_namespace,coalesce(activated_at,created_at) desc,id desc)",
		"where is_active and status in ('ready','partial')",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("generation 126 catalog revision index is missing %q", fragment)
		}
	}
}
