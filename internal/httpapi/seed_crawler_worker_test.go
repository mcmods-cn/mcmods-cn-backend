package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSeedCrawlerProjectTypesUseCanonicalInternalValues(t *testing.T) {
	values, ok := normalizeSeedCrawlerTypes([]string{"mod", "plugin", "shader", "resourcepack"})
	if !ok {
		t.Fatal("expected supported aliases to normalize")
	}
	want := []string{"mod", "plugin", "shader_pack", "resource_pack"}
	if len(values) != len(want) {
		t.Fatalf("normalized types = %v, want %v", values, want)
	}
	for index := range want {
		if values[index] != want[index] {
			t.Fatalf("normalized types = %v, want %v", values, want)
		}
	}
}

func TestSeedCrawlerDownloadThresholdIsStrict(t *testing.T) {
	if seedCrawlerDownloadEligible(100, 100) {
		t.Fatal("a project at the threshold must not be eligible")
	}
	if !seedCrawlerDownloadEligible(101, 100) {
		t.Fatal("a project above the threshold must be eligible")
	}
}

func TestSeedCrawlerTranslationUsesCanonicalLocalizationFieldsAndRejectsIncompleteResults(t *testing.T) {
	sourceLocale, items, err := seedDraftTranslationSource([]byte(`{
		"defaultLocale":"en-US","primaryName":"legacy top-level name",
		"localizations":[{"locale":"en-US","name":"Localized name","summary":"Localized summary","contentMarkdown":"Localized body"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if sourceLocale != "en-US" || len(items) != 3 || items[0]["key"] != "name" || items[0]["text"] != "Localized name" ||
		items[1]["key"] != "summary" || items[2]["key"] != "bodyMarkdown" || items[2]["text"] != "Localized body" {
		t.Fatalf("translation source locale/items = %q/%v", sourceLocale, items)
	}
	if _, err = validateSeedDraftTranslationResult(map[string]any{"items": []any{
		map[string]any{"key": "name", "text": "名称"},
		map[string]any{"key": "summary", "text": "简介"},
	}}, items); err == nil {
		t.Fatal("incomplete paid translation was accepted")
	}
	translated, err := validateSeedDraftTranslationResult(map[string]any{"items": []any{
		map[string]any{"key": "name", "text": "名称"},
		map[string]any{"key": "summary", "text": "简介"},
		map[string]any{"key": "bodyMarkdown", "text": "正文"},
	}}, items)
	if err != nil || translated["name"] != "名称" || translated["summary"] != "简介" || translated["bodyMarkdown"] != "正文" {
		t.Fatalf("validated translation = %v, err=%v", translated, err)
	}
}

func TestRandomSeedCrawlerOffsetStaysInsideCandidateWindow(t *testing.T) {
	for range 100 {
		offset, err := randomSeedCrawlerOffset(250, 25)
		if err != nil {
			t.Fatal(err)
		}
		if offset < 0 || offset > 225 {
			t.Fatalf("offset %d is outside [0,225]", offset)
		}
	}
	offset, err := randomSeedCrawlerOffset(10, 10)
	if err != nil || offset != 0 {
		t.Fatalf("full candidate window offset = %d, err = %v", offset, err)
	}
}

func TestSeedModrinthSearchURLUsesOfficialAPIFilters(t *testing.T) {
	endpoint, err := url.Parse(seedModrinthSearchURL("https://api.modrinth.com/v2/", "shader", 7, 13))
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Path != "/v2/search" || endpoint.Query().Get("limit") != "7" || endpoint.Query().Get("offset") != "13" || endpoint.Query().Get("index") != "downloads" {
		t.Fatalf("unexpected search URL: %s", endpoint.String())
	}
	if endpoint.Query().Get("facets") != `[["project_type:shader"]]` {
		t.Fatalf("unexpected facets: %s", endpoint.Query().Get("facets"))
	}
}

func TestFetchSeedModrinthProjectsProbesThenLoadsCandidatePage(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"total_hits": 1, "hits": []any{}})
			return
		}
		if r.URL.Query().Get("offset") != "0" || r.URL.Query().Get("facets") != `[["project_type:resourcepack"]]` {
			t.Errorf("unexpected page request: %s", r.URL.String())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"total_hits": 1, "hits": []map[string]any{{
			"project_id": "abc", "slug": "sample", "title": "Sample", "description": "Useful pack", "downloads": 101,
		}}})
	}))
	defer server.Close()

	cfg := defaultModImportConfig()
	cfg.Modrinth.BaseURL = server.URL
	hits, err := fetchSeedModrinthProjects(context.Background(), server.Client(), cfg, "resource_pack", 5)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(hits) != 1 || hits[0].ProjectID != "abc" {
		t.Fatalf("requests=%d hits=%+v", requests, hits)
	}
}
