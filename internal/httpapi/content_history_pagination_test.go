package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestContentHistoryRejectsInvalidPageBeforeDatabaseAccess(t *testing.T) {
	server := &Server{}
	for _, testCase := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		target  string
		path    map[string]string
	}{
		{name: "community", handler: server.communityPostHistory, target: "/api/v1/community/posts/post00001/history?limit=101", path: map[string]string{"id": "post00001"}},
		{name: "resource", handler: server.modContentResourceHistory, target: "/api/v1/mods/mod-one/content-resources/resource1/history?version=version01&offset=1", path: map[string]string{"siteId": "mod-one", "resourceId": "resource1"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, testCase.target, nil).WithContext(context.Background())
			for key, value := range testCase.path {
				request.SetPathValue(key, value)
			}
			response := httptest.NewRecorder()
			testCase.handler(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestContentHistoryCursorIsStrictAndBoundToTargetAndLimit(t *testing.T) {
	request, err := parseContentHistoryPageRequest(url.Values{"limit": {"25"}}, "community:post00001")
	if err != nil || request.Limit != 25 || request.Cursor != nil {
		t.Fatalf("initial request=%+v err=%v", request, err)
	}
	createdAt := time.Date(2026, 8, 22, 10, 11, 12, 123, time.UTC)
	raw := encodeContentHistoryPageCursor(contentHistoryPageCursor{
		Version: contentHistoryCursorVersion, Scope: request.Scope, CreatedAt: createdAt, Origin: "manual", ID: "revision1",
	})
	parsed, err := parseContentHistoryPageRequest(url.Values{"limit": {"25"}, "cursor": {raw}}, "community:post00001")
	if err != nil || parsed.Cursor == nil || !parsed.Cursor.CreatedAt.Equal(createdAt) || parsed.Cursor.ID != "revision1" {
		t.Fatalf("parsed request=%+v err=%v", parsed, err)
	}
	for name, values := range map[string]url.Values{
		"foreign target": {"limit": {"25"}, "cursor": {raw}},
		"foreign limit":  {"limit": {"50"}, "cursor": {raw}},
		"oversized page": {"limit": {"101"}},
		"offset alias":   {"offset": {"25"}},
		"duplicate":      {"limit": {"25", "50"}},
		"unknown":        {"all": {"true"}},
	} {
		target := "community:post00001"
		if name == "foreign target" {
			target = "community:post00002"
		}
		if _, parseErr := parseContentHistoryPageRequest(values, target); parseErr == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if _, err = decodeContentHistoryPageCursor(strings.Repeat("a", contentHistoryMaxCursorSize+1), request.Scope); err == nil {
		t.Fatal("oversized cursor was accepted")
	}
	if _, err = parseContentHistoryPageRequest(url.Values{"version": {"version01"}}, "resource:one", "version"); err != nil {
		t.Fatalf("allowed resource version parameter was rejected: %v", err)
	}
}

func TestContentHistoryMergeUsesStableCrossSourceOrderAndBoundedCursor(t *testing.T) {
	base := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	request, err := parseContentHistoryPageRequest(url.Values{"limit": {"3"}}, "resource:one:version01")
	if err != nil {
		t.Fatal(err)
	}
	manual := []contentHistoryItem{
		{ID: "manual-b", Origin: "manual", CreatedAt: base.Add(4 * time.Second)},
		{ID: "manual-a", Origin: "manual", CreatedAt: base.Add(2 * time.Second)},
	}
	imports := []contentHistoryItem{
		{ID: "import-b", Origin: "import", CreatedAt: base.Add(4 * time.Second)},
		{ID: "import-a", Origin: "import", CreatedAt: base.Add(3 * time.Second)},
		{ID: "import-0", Origin: "import", CreatedAt: base.Add(time.Second)},
	}
	page := mergeContentHistoryPage(manual, imports, request)
	if len(page.Items) != 3 || !page.HasMore || page.NextCursor == "" || page.Limit != 3 {
		t.Fatalf("page=%+v", page)
	}
	want := []string{"manual-b", "import-b", "import-a"}
	for index, id := range want {
		if page.Items[index].ID != id {
			t.Fatalf("items[%d]=%s want %s", index, page.Items[index].ID, id)
		}
	}
	cursor, err := decodeContentHistoryPageCursor(page.NextCursor, request.Scope)
	if err != nil || cursor == nil || cursor.Origin != "import" || cursor.ID != "import-a" {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
}
