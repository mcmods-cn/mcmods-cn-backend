package searchindex

import (
	"os"
	"strings"
	"testing"
)

func TestRebuildStreamsStablePreaggregatedBudgetedPages(t *testing.T) {
	source, err := os.ReadFile("worker.go")
	if err != nil {
		t.Fatal(err)
	}
	registrySource, err := os.ReadFile("schema.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(source) + "\n" + string(registrySource))
	if !strings.Contains(text, "func (worker *worker) rebuild(") {
		t.Fatal("could not find search rebuild")
	}
	for _, forbidden := range []string{
		"loaddocuments(ctx, kind, nil)",
		"for start := 0; start < len(documents)",
		"func (worker *worker) loaddocuments(",
		"return ids == nil",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("search rebuild still materializes a collection via %q", forbidden)
		}
	}
	for _, required := range []string{
		"searchrebuildpagesize", "searchimportbytebudget", "loaddocumentidpage",
		"importsearchdocumentbatches", "recordsearchrebuildprogress", "document.id>$1",
		"with selected_mods as", "localizations as", "group by",
		"left(mod.body_markdown,8192)", ")[1:16]", "keyword.ordinal<=64",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("bounded search rebuild is missing %q", required)
		}
	}
}
