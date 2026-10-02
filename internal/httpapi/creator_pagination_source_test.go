package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestCreatorCatalogExposesBoundedCursorPagination(t *testing.T) {
	handler, err := os.ReadFile("creator_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	pagination, err := os.ReadFile("creator_pagination.go")
	if err != nil {
		t.Fatal(err)
	}
	combined := string(handler) + string(pagination)
	for _, required := range []string{
		"decodeCreatorPageCursor",
		"encodeCreatorPageCursor",
		"creatorCursorPredicateSQL",
		`values.Get("cursor")`,
		`"nextCursor"`,
		`"hasMore"`,
	} {
		if !strings.Contains(combined, required) {
			t.Errorf("creator pagination is missing %q", required)
		}
	}
	if !strings.Contains(combined, "request.Limit + 1") {
		t.Error("SQL creator pages do not fetch one bounded lookahead row")
	}
}
