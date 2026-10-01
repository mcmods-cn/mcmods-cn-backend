package database

import (
	"strings"
	"testing"
)

func TestCommunityPostCatalogProjectionGeneration133(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168", schemaGeneration)
	}
	source := strings.ToLower(strings.Join(communityPostCatalogSchemaStatements(), "\n"))
	for _, required := range []string{
		"community_post_catalog", "body_summary", "search_document", "using gin(search_document)",
		"idx_community_post_catalog_published", "idx_community_post_catalog_updated",
		"idx_community_post_catalog_heat", "refresh_community_post_catalog",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("schema missing %q", required)
		}
	}
}
