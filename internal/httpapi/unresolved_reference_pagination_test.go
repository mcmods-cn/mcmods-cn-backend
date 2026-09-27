package httpapi

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestUnresolvedReferenceCursorIsStrictAndScopeBound(t *testing.T) {
	t.Parallel()
	request, err := parseUnresolvedReferencePageRequest(url.Values{
		"q": {"Mine_Craft%"}, "type": {"mod"}, "status": {"pending"}, "limit": {"17"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.Query != "mine_craft%" || request.SearchPrefix != `mine\_craft\%%` {
		t.Fatalf("literal prefix normalization=%q/%q", request.Query, request.SearchPrefix)
	}
	cursor := encodeUnresolvedReferencePageCursor(unresolvedReferencePageCursor{
		Version: unresolvedReferencePageCursorVersion, Scope: request.Scope,
		CreatedAt: time.Date(2026, 8, 23, 1, 2, 3, 0, time.UTC), Origin: 1, SourceRowID: 91,
	})
	valid := url.Values{"q": {"mine_craft%"}, "type": {"mod"}, "status": {"pending"}, "limit": {"17"}, "cursor": {cursor}}
	if _, err = parseUnresolvedReferencePageRequest(valid); err != nil {
		t.Fatalf("valid cursor rejected: %v", err)
	}
	for label, values := range map[string]url.Values{
		"offset":       {"offset": {"1"}},
		"page":         {"page": {"2"}},
		"unknown":      {"other": {"1"}},
		"duplicate":    {"limit": {"17", "18"}},
		"short search": {"q": {"x"}},
		"cross query":  {"q": {"other"}, "type": {"mod"}, "status": {"pending"}, "limit": {"17"}, "cursor": {cursor}},
		"cross type":   {"q": {"mine_craft%"}, "type": {"tag"}, "status": {"pending"}, "limit": {"17"}, "cursor": {cursor}},
		"cross status": {"q": {"mine_craft%"}, "type": {"mod"}, "status": {"resolved"}, "limit": {"17"}, "cursor": {cursor}},
		"cross limit":  {"q": {"mine_craft%"}, "type": {"mod"}, "status": {"pending"}, "limit": {"18"}, "cursor": {cursor}},
	} {
		if _, parseErr := parseUnresolvedReferencePageRequest(values); parseErr == nil {
			t.Fatalf("%s request was accepted", label)
		}
	}
}

func TestUnresolvedReferencePageUsesProjectionKeysetAndBoundedSearch(t *testing.T) {
	t.Parallel()
	request, err := parseUnresolvedReferencePageRequest(url.Values{
		"q": {"minecraft"}, "type": {"mod"}, "status": {"pending"}, "limit": {"50"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor = &unresolvedReferencePageCursor{
		Version: unresolvedReferencePageCursorVersion, Scope: request.Scope,
		CreatedAt: time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC), Origin: 1, SourceRowID: 100,
	}
	pageSQL, _ := buildUnresolvedReferencePageQuery(request)
	budgetSQL, _ := buildUnresolvedReferenceSearchBudgetQuery(request)
	combined := strings.ToLower(pageSQL + "\n" + budgetSQL)
	for _, required := range []string{
		"from unresolved_reference_catalog", "(created_at,origin,source_row_id)<", "order by created_at desc,origin desc,source_row_id desc",
		"lower(raw_identifier) like", "lower(source_label) like", "escape", "limit",
	} {
		if !strings.Contains(combined, required) {
			t.Fatalf("unresolved reference SQL is missing %q", required)
		}
	}
	for _, forbidden := range []string{" ilike ", " offset ", "count(*) over", "unresolved_resource_references unresolved"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("unresolved reference SQL retains %q", forbidden)
		}
	}
}

func TestUnresolvedReferenceHandlerRemovesWindowCountAndOffset(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("unresolved_reference_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(source))
	for _, forbidden := range []string{"boundedoffset", "count(*) over", "\"total\"", "\"offset\""} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("unresolved reference handler retains %q", forbidden)
		}
	}
	for _, required := range []string{"hasmore", "nextcursor", "rows.err()"} {
		if !strings.Contains(text, required) {
			t.Fatalf("unresolved reference handler is missing %q", required)
		}
	}
}
