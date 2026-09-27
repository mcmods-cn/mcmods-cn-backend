package httpapi

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestAutomaticCatalogImportsOnlyReactivatePlaceholders(t *testing.T) {
	files := []string{
		"catalog_import.go",
		"mod_export_handlers.go",
		"mod_export_import_handlers.go",
		"mod_export_recipe_batch.go",
		"mod_export_recipe_import.go",
	}
	activation := regexp.MustCompile(`(?s)on conflict\(identity_key\) do update set status='active'.{0,180}`)
	matchCount := 0
	for _, name := range files {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range activation.FindAllString(string(raw), -1) {
			matchCount++
			if !strings.Contains(match, "catalog_entities.status='placeholder'") {
				t.Fatalf("%s contains an automatic activation without placeholder governance:\n%s", name, match)
			}
		}
	}
	if matchCount != 7 {
		t.Fatalf("automatic catalog activation inventory changed: got %d, want 7", matchCount)
	}
	raw, err := os.ReadFile("catalog_import.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "entity.status<>'active'") {
		t.Fatal("resource import still reactivates every non-active governance state")
	}
	manualPublish, err := os.ReadFile("catalog_editor_service.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manualPublish), "update catalog_entities set status='active',archived_at=null") {
		t.Fatal("explicit catalog publication no longer owns archived entity restoration")
	}
}

func TestMaterializedImportEntriesUseStrictNamespaceResolution(t *testing.T) {
	required := map[string][]string{
		"mod_export_documents.go":       {"exportRevisionForNamespace(revisions, entry.Namespace)"},
		"mod_export_import_handlers.go": {"exportRevisionForNamespace(revisions, namespace)"},
		"mod_export_block_bindings.go":  {"exportRevisionForNamespace(revisions, block.Namespace)"},
		"mod_export_block_entities.go":  {"exportRevisionForNamespace(revisions, namespace)"},
		"mod_export_recipe_import.go":   {"exportRevisionForRecipeType(revisions, category.RecipeTypeID)", "exportRevisionForRecipeType(revisions, recipeIndex.RecipeTypeID)", "exportRevisionForRecipeType(revisions, recipeTypeID)"},
	}
	for name, needles := range required {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		contents := string(raw)
		for _, needle := range needles {
			if !strings.Contains(contents, needle) {
				t.Errorf("%s no longer uses strict namespace attribution %q", name, needle)
			}
		}
	}
	recipeRaw, err := os.ReadFile("mod_export_recipe_import.go")
	if err != nil {
		t.Fatal(err)
	}
	documentRaw, err := os.ReadFile("mod_export_documents.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(recipeRaw), "len(revisions) == 1") || strings.Contains(string(documentRaw), "len(revisions) == 1") {
		t.Fatal("single-revision namespace fallback was reintroduced")
	}
	if strings.Contains(string(documentRaw), "_ = ordinal") || strings.Contains(string(documentRaw), "for ordinal, entry := range entries") {
		t.Fatal("document import reintroduced a semantically dead ordinal")
	}
}
