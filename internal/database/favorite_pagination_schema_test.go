package database

import (
	"strings"
	"testing"
)

func TestFavoritePaginationSchemaGenerationAndIndexes(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("schema generation=%d want 167", schemaGeneration)
	}
	source := strings.ToLower(strings.Join(favoritePaginationSchemaStatements(), "\n"))
	for _, required := range []string{
		"idx_favorite_collections_user_page",
		"on favorite_collections(user_id,is_default desc,created_at,id)",
		"idx_favorite_collections_public_page",
		"where is_public",
		"idx_favorite_items_collection_page",
		"on favorite_collection_items(collection_id,created_at desc,id desc)",
		"idx_favorite_items_target_collection",
		"on favorite_collection_items(entity_type,entity_id,collection_id)",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("favorite pagination schema missing %q", required)
		}
	}
}
