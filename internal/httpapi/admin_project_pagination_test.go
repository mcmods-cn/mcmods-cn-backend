package httpapi

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAdminProjectPageCursorIsStrictAndFilterScoped(t *testing.T) {
	request, err := parseAdminProjectPageRequest(url.Values{
		"q": {" Searchable "}, "type": {"server"}, "limit": {"7"},
	})
	if err != nil || request.Query != "searchable" || request.ProjectType != "minecraft_server" || request.Limit != 7 {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	cursor := encodeAdminProjectPageCursor(adminProjectPageCursor{
		Version: adminProjectPageCursorVersion, Scope: request.Scope, Heat: "12.340000",
		UpdatedAt: time.Date(2026, 8, 22, 1, 2, 3, 0, time.UTC), RouteID: 42,
	})
	values := url.Values{"q": {"SEARCHABLE"}, "type": {"minecraft_server"}, "limit": {"7"}, "cursor": {cursor}}
	next, err := parseAdminProjectPageRequest(values)
	if err != nil || next.Cursor == nil || next.Cursor.RouteID != 42 || next.Cursor.Heat != "12.340000" {
		t.Fatalf("next=%+v err=%v", next, err)
	}
	for label, mutate := range map[string]func(url.Values){
		"query": func(candidate url.Values) { candidate.Set("q", "different") },
		"type":  func(candidate url.Values) { candidate.Set("type", "mod") },
		"limit": func(candidate url.Values) { candidate.Set("limit", "8") },
	} {
		candidate := cloneAdminProjectURLValues(values)
		mutate(candidate)
		if _, parseErr := parseAdminProjectPageRequest(candidate); parseErr == nil {
			t.Fatalf("cursor was reusable across %s scope", label)
		}
	}
	unknownField := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"scope":"` + request.Scope + `","heat":"1","updatedAt":"2026-08-22T00:00:00Z","routeId":1,"extra":true}`))
	invalidHeat := encodeAdminProjectPageCursor(adminProjectPageCursor{
		Version: adminProjectPageCursorVersion, Scope: request.Scope, Heat: strings.Repeat("9", 1000),
		UpdatedAt: time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC), RouteID: 1,
	})
	for label, candidate := range map[string]url.Values{
		"offset":        {"offset": {"1"}},
		"page":          {"page": {"2"}},
		"repeated":      {"q": {"one", "two"}},
		"unknown field": {"q": {"searchable"}, "type": {"server"}, "limit": {"7"}, "cursor": {unknownField}},
		"invalid heat":  {"q": {"searchable"}, "type": {"server"}, "limit": {"7"}, "cursor": {invalidHeat}},
	} {
		if _, parseErr := parseAdminProjectPageRequest(candidate); parseErr == nil {
			t.Fatalf("%s request was accepted", label)
		}
	}
}

func TestAdminProjectPageSQLUsesSearchAndCompositeKeyset(t *testing.T) {
	request, err := parseAdminProjectPageRequest(url.Values{"q": {"needle"}, "type": {"mod"}, "limit": {"40"}})
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor = &adminProjectPageCursor{
		Version: adminProjectPageCursorVersion, Scope: request.Scope, Heat: "5.250000",
		UpdatedAt: time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC), RouteID: 99,
	}
	query, arguments := adminProjectPageSQL(request)
	lower := strings.ToLower(query)
	for _, required := range []string{
		"from admin_project_catalog", "search_document @@ websearch_to_tsquery('simple'",
		"(project.heat_score,project.updated_at,project.object_route_id)<", "order by project.heat_score desc",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("query is missing %q: %s", required, query)
		}
	}
	for _, forbidden := range []string{" count(", " ilike ", " offset "} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("query retained %q: %s", forbidden, query)
		}
	}
	if len(arguments) != 6 || arguments[len(arguments)-1] != 41 {
		t.Fatalf("arguments=%#v", arguments)
	}
}

func cloneAdminProjectURLValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, items := range values {
		cloned[key] = append([]string(nil), items...)
	}
	return cloned
}
