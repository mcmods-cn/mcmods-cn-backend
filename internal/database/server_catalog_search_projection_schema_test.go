package database

import (
	"strings"
	"testing"
)

func TestServerCatalogPopularityRefreshQueuesSearchProjection(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168", schemaGeneration)
	}
	definition := strings.Join(searchSchemaStatements(), "\n")
	for _, expected := range []string{
		"enqueue_search_server_popularity()",
		"route.entity_type='minecraft_server'",
		"enqueue_search_index_document('server',route.internal_id,'upsert')",
		"trg_search_server_popularity",
		"after insert or update on content_popularity_stats",
	} {
		if !strings.Contains(definition, expected) {
			t.Fatalf("search schema is missing %q", expected)
		}
	}
}

func TestServerCatalogSQLFallbackHasKeysetIndexes(t *testing.T) {
	definition := strings.Join(serverSchemaStatements(), "\n")
	for _, expected := range []string{
		"idx_minecraft_servers_public_updated",
		"on minecraft_servers(updated_at desc,id desc) where review_status='approved'",
		"idx_minecraft_servers_public_created",
		"on minecraft_servers(created_at desc,updated_at desc,id desc) where review_status='approved'",
		"idx_minecraft_servers_public_name",
		"on minecraft_servers(lower(name),id) where review_status='approved'",
	} {
		if !strings.Contains(definition, expected) {
			t.Fatalf("server schema is missing %q", expected)
		}
	}
}
