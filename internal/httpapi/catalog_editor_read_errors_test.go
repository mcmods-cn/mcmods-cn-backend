package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestCatalogEditorDetailsDoNotTurnDatabaseFailuresIntoEmptyFacts(t *testing.T) {
	editorRaw, err := os.ReadFile("catalog_editor_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	recipeRaw, err := os.ReadFile("catalog_recipe_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	editorSource, recipeSource := string(editorRaw), string(recipeRaw)
	for _, function := range []struct {
		name   string
		source string
	}{
		{name: "catalogRecipeTypeDetail", source: editorSource},
		{name: "catalogRecipeTemplateDetail", source: editorSource},
		{name: "catalogRecipeDetail", source: recipeSource},
	} {
		body := goFunctionBody(t, function.source, function.name)
		for _, forbidden := range []string{
			"_ = s.db.QueryRow",
			"localizations, _ :=",
			"slots, _ :=",
			"catalysts, _ :=",
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s still hides a database failure through %q", function.name, forbidden)
			}
		}
		if !strings.Contains(body, "reviewErr") || !strings.Contains(body, "http.StatusInternalServerError") {
			t.Fatalf("%s must reject a review-status database failure before writing its detail", function.name)
		}
		if !strings.Contains(body, "logCatalogEditorReadFailure") {
			t.Fatalf("%s must record query context for a required-read failure", function.name)
		}
	}

	statusBody := goFunctionBody(t, recipeSource, "catalogPendingReviewStatus")
	for _, required := range []string{"(string, error)", "errors.Is(err, pgx.ErrNoRows)", "return \"\", err"} {
		if !strings.Contains(statusBody, required) {
			t.Fatalf("review status must distinguish an absent request from a database failure; missing %q", required)
		}
	}
	activeBody := goFunctionBody(t, recipeSource, "catalogRecipeTypeIsActive")
	if !strings.Contains(activeBody, "(bool, error)") {
		t.Fatal("recipe type activity lookup still turns every database failure into inactive")
	}
}
