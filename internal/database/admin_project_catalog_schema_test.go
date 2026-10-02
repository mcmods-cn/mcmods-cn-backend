package database

import (
	"strings"
	"testing"
)

func TestAdminProjectCatalogHasGeneration125SearchAndSortProjection(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(adminDashboardSchemaStatements(), "\n"))
	for _, required := range []string{
		"create table admin_project_catalog",
		"search_document tsvector",
		"idx_admin_project_catalog_search",
		"using gin(search_document)",
		"idx_admin_project_catalog_heat",
		"idx_admin_project_catalog_type_heat",
		"refresh_admin_project_catalog",
		"trg_admin_project_catalog_routes",
		"trg_admin_project_catalog_metrics",
		"trg_admin_project_catalog_popularity",
	} {
		if !strings.Contains(schema, required) {
			t.Errorf("admin project catalog schema is missing %q", required)
		}
	}
}
