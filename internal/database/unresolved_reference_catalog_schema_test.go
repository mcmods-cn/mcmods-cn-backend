package database

import (
	"strings"
	"testing"
)

func TestUnresolvedReferenceCatalogUsesGeneration135BoundedProjection(t *testing.T) {
	t.Parallel()
	if schemaGeneration != 167 {
		t.Fatalf("schema generation=%d want 167", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(unresolvedReferenceCatalogSchemaStatements(), "\n"))
	for _, required := range []string{
		"create table unresolved_reference_catalog",
		"primary key(origin,source_row_id)",
		"idx_unresolved_reference_catalog_page",
		"idx_unresolved_reference_catalog_status_page",
		"idx_unresolved_reference_catalog_type_page",
		"idx_unresolved_reference_catalog_status_type_page",
		"idx_unresolved_reference_catalog_raw_prefix",
		"idx_unresolved_reference_catalog_label_prefix",
		"text_pattern_ops",
		"refresh_unresolved_reference_catalog_general",
		"refresh_unresolved_reference_catalog_resource",
		"trg_unresolved_reference_catalog_general",
		"trg_unresolved_reference_catalog_resource",
		"unresolved.source_type='minecraft_server_mod'",
		"unresolved.source_type='simple_project_parent'",
		"unresolved.source_type='mod_content_resource'",
		"trg_unresolved_reference_catalog_server_mod_link",
		"trg_unresolved_reference_catalog_simple_parent_link",
		"trg_unresolved_reference_catalog_server_label",
		"trg_unresolved_reference_catalog_simple_project_label",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("generation 135 unresolved reference projection is missing %q", required)
		}
	}
}
