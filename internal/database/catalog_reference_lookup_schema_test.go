package database

import (
	"strings"
	"testing"
)

func TestCatalogReferenceLookupHasCaseFoldedIndexes(t *testing.T) {
	t.Parallel()
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168", schemaGeneration)
	}
	schemaSQL := strings.Join(catalogSchemaStatements(), "\n")
	for _, required := range []string{
		"idx_game_resources_kind_canonical_folded",
		"game_resources(kind_code,lower(canonical_id),entity_id)",
		"idx_game_resource_aliases_kind_alias_folded",
		"game_resource_aliases(kind_code,lower(alias_id),resource_id)",
		"idx_catalog_tags_canonical_registry_folded",
		"catalog_tags(lower(canonical_id),lower(registry),entity_id)",
	} {
		if !strings.Contains(schemaSQL, required) {
			t.Errorf("catalog reference lookup schema is missing %q", required)
		}
	}
}
