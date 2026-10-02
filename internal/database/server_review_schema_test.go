package database

import (
	"strings"
	"testing"
)

func TestServerReviewPageIndexIsInstalledInCurrentSchema(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("unexpected schema generation %d", schemaGeneration)
	}
	schema := strings.Join(serverSchemaStatements(), "\n")
	if !strings.Contains(schema, "idx_minecraft_servers_review_page") ||
		!strings.Contains(schema, "minecraft_servers(review_status,created_at,id)") {
		t.Fatal("server review keyset page index is missing")
	}
}
