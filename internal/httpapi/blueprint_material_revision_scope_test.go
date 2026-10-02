package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestBlueprintMaterialRevisionsAreLimitedToUsedNamespaces(t *testing.T) {
	handlerBytes, err := os.ReadFile("blueprint_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	handler := strings.ToLower(string(handlerBytes))
	if !strings.Contains(handler, "source_namespace=any($1::text[])") {
		t.Fatal("blueprint material revision lookup is not limited to the blueprint's namespace array")
	}
	if !strings.Contains(handler, "order by source_namespace,coalesce(activated_at,created_at) desc,id desc") {
		t.Fatal("blueprint material revision lookup has no deterministic latest-revision order")
	}

	migrationBytes, err := os.ReadFile("../database/migrations.go")
	if err != nil {
		t.Fatal(err)
	}
	migrations := strings.ToLower(string(migrationBytes))
	if !strings.Contains(migrations, "idx_catalog_import_revisions_material_namespace") ||
		!strings.Contains(migrations, "where is_active and status in ('ready','partial')") {
		t.Fatal("catalog import revisions have no matching active namespace/latest-time index")
	}
}

func TestBlueprintMaterialNamespacesAreDeduplicatedBeforeLookup(t *testing.T) {
	namespaces := make([]string, 0, 4)
	seen := make(map[string]struct{})
	for _, blockID := range []string{"minecraft:stone", "minecraft:dirt", "create:shaft", "minecraft:glass", ":invalid"} {
		namespaces = appendBlueprintMaterialNamespace(namespaces, seen, blockID)
	}
	if got := strings.Join(namespaces, ","); got != "minecraft,create" {
		t.Fatalf("deduplicated namespaces = %q, want minecraft,create", got)
	}
}
