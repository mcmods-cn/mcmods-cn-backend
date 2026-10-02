package searchindex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestSearchUsesAliasAndDecodesFacets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-TYPESENSE-API-KEY") != "test-key" {
			t.Fatalf("missing Typesense API key")
		}
		if r.URL.Path != "/collections/test_creators/documents/search" {
			t.Fatalf("unexpected search path %q", r.URL.Path)
		}
		if r.URL.Query().Get("facet_by") != "kind" || r.URL.Query().Get("filter_by") != "review_status:=approved" {
			t.Fatalf("unexpected search query %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"found":2,"hits":[{"document":{"internal_id":17}},{"document":{"internal_id":23}}],"facet_counts":[{"field_name":"kind","counts":[{"value":"author","count":1},{"value":"team","count":1}]}]}`))
	}))
	defer server.Close()

	client := New(config.TypesenseConfig{Enabled: true, URL: server.URL, APIKey: "test-key", CollectionPrefix: "test", Timeout: time.Second})
	client.SetReady(true)
	result, err := client.Search(context.Background(), SearchRequest{
		Collection: "creators", Query: "mek", QueryBy: []string{"name", "text"},
		FilterBy: "review_status:=approved", FacetBy: []string{"kind"}, Page: 1, PerPage: 20,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.Found != 2 || len(result.IDs) != 2 || result.IDs[0] != 17 || result.IDs[1] != 23 {
		t.Fatalf("unexpected search result %#v", result)
	}
	if result.Facets["kind"]["author"] != 1 || result.Facets["kind"]["team"] != 1 {
		t.Fatalf("unexpected facets %#v", result.Facets)
	}
}

func TestImportDocumentsReportsPerDocumentFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/projects/documents/import" {
			t.Fatalf("unexpected import request %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("action") != "upsert" || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/plain") {
			t.Fatalf("unexpected import metadata")
		}
		_, _ = w.Write([]byte("{\"success\":true}\n{\"success\":false,\"error\":\"bad document\"}\n"))
	}))
	defer server.Close()

	client := New(config.TypesenseConfig{Enabled: true, URL: server.URL, APIKey: "test-key", Timeout: time.Second})
	err := client.ImportDocuments(context.Background(), "projects", []map[string]any{{"id": "mod_1"}, {"id": "mod_2"}})
	if err == nil || !strings.Contains(err.Error(), "bad document") {
		t.Fatalf("ImportDocuments() error = %v", err)
	}
}

func TestSearchDocumentIDPageQueriesUseStableKeysets(t *testing.T) {
	for _, documentType := range []string{"mod", "modpack", "simple_project", "community_post", "creator", "resource", "server"} {
		query, err := searchDocumentIDPageQuery(documentType)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(query, ">$1") || !strings.Contains(query, "order by") || !strings.Contains(query, "limit $2") {
			t.Errorf("%s ID page is not a stable keyset: %s", documentType, query)
		}
	}
}

func TestSearchMarksClientUnavailableOnServerFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client := New(config.TypesenseConfig{Enabled: true, URL: server.URL, APIKey: "test-key", CollectionPrefix: "test", Timeout: time.Second})
	client.SetReady(true)
	_, err := client.Search(context.Background(), SearchRequest{Collection: "projects", Query: "test", QueryBy: []string{"names"}})
	if err == nil || client.Ready() {
		t.Fatalf("Search() error = %v, ready = %v", err, client.Ready())
	}
}
