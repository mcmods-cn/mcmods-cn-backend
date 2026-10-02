package database

import (
	"strings"
	"testing"
)

func TestCatalogSchemaDoesNotDuplicateUniqueConstraintIndexes(t *testing.T) {
	catalog := strings.ToLower(strings.Join(catalogSchemaStatements(), "\n"))
	editor := strings.ToLower(strings.Join(catalogEditorSchemaStatements(), "\n"))
	modContent := strings.ToLower(strings.Join(modContentSchemaStatements(), "\n"))
	for _, constraint := range []struct {
		schema string
		value  string
	}{
		{catalog, "unique(resource_id,revision_id)"},
		{catalog, "unique(revision_id,registry,resource_id)"},
		{editor, "unique(recipe_type_id,template_key)"},
		{modContent, "unique(version_id,parent_id,ordinal)"},
	} {
		if !strings.Contains(constraint.schema, constraint.value) {
			t.Errorf("required uniqueness is missing: %s", constraint.value)
		}
	}
	for _, duplicate := range []struct {
		schema string
		name   string
	}{
		{catalog, "idx_resource_import_snapshots_resource"},
		{catalog, "idx_resource_import_snapshots_revision_registry"},
		{editor, "idx_recipe_layout_templates_type"},
		{modContent, "idx_mod_content_sections_tree"},
	} {
		if strings.Contains(duplicate.schema, duplicate.name) {
			t.Errorf("redundant unique-prefix index remains: %s", duplicate.name)
		}
	}
}
