package httpapi

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSkinCatalogCursorIsStrictAndFilterScoped(t *testing.T) {
	t.Parallel()
	request, err := parseSkinCatalogPageRequest(url.Values{
		"q": {"red cape"}, "kind": {"cape"}, "sort": {"downloads"}, "order": {"desc"}, "limit": {"36"},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := encodeSkinCatalogPageCursor(skinCatalogPageCursor{
		Version: skinCatalogPageCursorVersion, Scope: request.Scope, Sort: request.Sort, Direction: request.Direction,
		ID: 42, UpdatedAt: time.Date(2026, 8, 22, 1, 2, 3, 456000000, time.UTC), Metric: "123",
	})
	requestWithCursor, err := parseSkinCatalogPageRequest(url.Values{
		"q": {"red cape"}, "kind": {"cape"}, "sort": {"downloads"}, "order": {"desc"}, "limit": {"36"}, "cursor": {raw},
	})
	if err != nil || requestWithCursor.Cursor == nil || requestWithCursor.Cursor.ID != 42 {
		t.Fatalf("cursor=%+v err=%v", requestWithCursor.Cursor, err)
	}
	for name, values := range map[string]url.Values{
		"cross filter": {"q": {"blue cape"}, "kind": {"cape"}, "sort": {"downloads"}, "order": {"desc"}, "limit": {"36"}, "cursor": {raw}},
		"old offset":   {"offset": {"36"}},
		"old page":     {"page": {"2"}},
		"unknown":      {"surprise": {"1"}},
		"duplicate":    {"limit": {"20", "30"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, parseErr := parseSkinCatalogPageRequest(values); parseErr == nil {
				t.Fatal("invalid skin catalog request was accepted")
			}
		})
	}
	unknown := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"s":"x","sort":"published","direction":"desc","id":1,"createdAt":"2026-08-22T00:00:00Z","updatedAt":"2026-08-22T00:00:00Z","extra":true}`))
	if _, err = decodeSkinCatalogPageCursor(unknown, "x", catalogSortPublished, catalogSortDescending); err == nil {
		t.Fatal("cursor with unknown fields was accepted")
	}
}

func containsFolded(value, fragment string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(fragment))
}

func TestSkinCatalogPageSQLUsesProjectionSearchAndKeyset(t *testing.T) {
	t.Parallel()
	request, err := parseSkinCatalogPageRequest(url.Values{
		"q": {"red cape"}, "model": {"default"}, "sort": {"views"}, "order": {"asc"}, "limit": {"24"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor = &skinCatalogPageCursor{
		Version: skinCatalogPageCursorVersion, Scope: request.Scope, Sort: request.Sort, Direction: request.Direction,
		ID: 99, UpdatedAt: time.Now().UTC(), Metric: "1000",
	}
	query, arguments := skinCatalogPageSQL(request)
	for _, required := range []string{
		"from skin_public_catalog catalog", "search_document@@websearch_to_tsquery('simple'", "(catalog.view_count,catalog.updated_at,catalog.asset_id)>",
		"order by catalog.view_count asc,catalog.updated_at asc,catalog.asset_id asc", "limit",
	} {
		if !containsFolded(query, required) {
			t.Fatalf("skin catalog SQL is missing %q:\n%s", required, query)
		}
	}
	for _, forbidden := range []string{" offset ", "count(*)", "ilike", "array_to_string"} {
		if containsFolded(query, forbidden) {
			t.Fatalf("skin catalog SQL still contains %q:\n%s", forbidden, query)
		}
	}
	if len(arguments) == 0 || arguments[len(arguments)-1] != request.Limit+1 {
		t.Fatalf("arguments=%v want terminal limit+1", arguments)
	}
}
