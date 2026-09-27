package database

import (
	"strings"
	"testing"
)

func TestSkinCatalogProjectionHasGeneration130SearchAndKeysetIndexes(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("schema generation=%d want 167", schemaGeneration)
	}
	source := strings.ToLower(strings.Join(skinCatalogSchemaStatements(), "\n"))
	for _, required := range []string{
		"create table skin_public_catalog", "search_document tsvector", "using gin(search_document)",
		"idx_skin_public_catalog_published", "idx_skin_public_catalog_updated", "idx_skin_public_catalog_heat",
		"idx_skin_public_catalog_downloads", "idx_skin_public_catalog_favorites", "idx_skin_public_catalog_rating",
		"idx_skin_public_catalog_views", "idx_skin_public_catalog_comments", "idx_skin_public_catalog_name",
		"refresh_skin_public_catalog", "rebuild_skin_public_catalog", "trg_skin_public_catalog_assets",
		"trg_skin_public_catalog_routes", "trg_skin_public_catalog_popularity",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("Generation 133 skin catalog schema is missing %q", required)
		}
	}
}
