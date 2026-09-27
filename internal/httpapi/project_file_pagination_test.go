package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestProjectFilePageCursorBindsProjectSourceAndLimit(t *testing.T) {
	project := projectFileContext{ProjectType: "mod", ProjectID: "mod000001"}
	request, err := parseProjectFilePageRequest(url.Values{"source": {"internal"}, "limit": {"20"}}, project)
	if err != nil {
		t.Fatal(err)
	}
	raw := encodeProjectFilePageCursor(projectFilePageCursor{
		Version: projectFileCursorVersion, Scope: request.Scope, CreatedAt: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC), InternalID: 42,
	})
	if _, err = parseProjectFilePageRequest(url.Values{"source": {"internal"}, "limit": {"20"}, "cursor": {raw}}, project); err != nil {
		t.Fatal(err)
	}
	for _, values := range []url.Values{
		{"source": {"internal"}, "limit": {"21"}, "cursor": {raw}},
		{"source": {"modrinth"}, "limit": {"20"}, "cursor": {raw}},
		{"source": {"internal"}, "limit": {"20"}, "cursor": {raw}, "offset": {"20"}},
		{"source": {"internal"}, "limit": {"51"}},
	} {
		if _, parseErr := parseProjectFilePageRequest(values, project); parseErr == nil {
			t.Fatalf("expected request to fail: %#v", values)
		}
	}
}

func TestModrinthProjectFilePagesTraverseBoundedVersionBatches(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/project/example":
			versions := make([]string, 25)
			for index := range versions {
				versions[index] = fmt.Sprintf("v%02d", index+1)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "project01", "versions": versions})
		case "/versions":
			var ids []string
			if err := json.Unmarshal([]byte(r.URL.Query().Get("ids")), &ids); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if len(ids) > projectFileModrinthVersionBatchSize {
				t.Errorf("received an oversized version batch: %d", len(ids))
			}
			sort.Strings(ids)
			response := make([]map[string]any, 0, len(ids))
			for _, id := range ids {
				response = append(response, map[string]any{
					"id": id, "project_id": "project01", "name": id, "version_number": id,
					"version_type": "release", "date_published": "2026-08-20T12:00:00Z",
					"game_versions": []string{"1.21.1"}, "loaders": []string{"fabric"},
					"files": []map[string]any{{
						"hashes": map[string]string{"sha1": "hash-" + id}, "url": "https://cdn.example.test/" + id + ".jar",
						"filename": id + ".jar", "primary": true, "size": 1024,
					}},
				})
			}
			_ = json.NewEncoder(w).Encode(response)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	api := &Server{cache: querycache.New(config.RedisConfig{})}
	cfg := defaultModImportConfig()
	cfg.Modrinth.BaseURL = server.URL
	cfg.RequestTimeoutSeconds = 5
	project := projectFileContext{ProjectType: "mod", ProjectID: "mod000001", ModrinthProjectID: "example"}
	cursor := ""
	ids := make([]string, 0, 25)
	for pageNumber := 0; pageNumber < 10; pageNumber++ {
		request, err := parseProjectFilePageRequest(url.Values{"source": {"modrinth"}, "limit": {"7"}, "cursor": {cursor}}, project)
		if err != nil {
			t.Fatal(err)
		}
		page, err := api.loadModrinthProjectFilePage(context.Background(), project, request, cfg)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			ids = append(ids, item.ID)
		}
		if !page.HasMore {
			break
		}
		if page.NextCursor == "" {
			t.Fatal("page with more results did not provide a cursor")
		}
		cursor = page.NextCursor
	}
	if len(ids) != 25 {
		t.Fatalf("traversed %d files, want 25: %#v", len(ids), ids)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate file %q", id)
		}
		seen[id] = true
	}
	if ids[0] != "hash-v25" || ids[len(ids)-1] != "hash-v01" {
		t.Fatalf("unexpected provider order: first=%q last=%q", ids[0], ids[len(ids)-1])
	}
	if requests.Load() > 9 {
		t.Fatalf("provider request count was not bounded per page: %d", requests.Load())
	}
}

func TestCurseForgeProjectFilePagesUseLimitPlusOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mods/123/files" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		index, _ := strconv.Atoi(r.URL.Query().Get("index"))
		pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
		if pageSize != 21 {
			t.Errorf("pageSize=%d, want 21", pageSize)
		}
		end := min(index+pageSize, 45)
		data := make([]map[string]any, 0, end-index)
		for item := index; item < end; item++ {
			data = append(data, map[string]any{
				"id": item + 1, "displayName": fmt.Sprintf("File %d", item+1), "fileName": fmt.Sprintf("file-%d.jar", item+1),
				"releaseType": 1, "fileDate": "2026-08-20T12:00:00Z", "fileLength": 1024, "gameVersions": []string{"1.21.1", "Fabric"},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": data, "pagination": map[string]any{"index": index, "pageSize": pageSize, "resultCount": len(data), "totalCount": 45},
		})
	}))
	defer server.Close()

	cfg := defaultModImportConfig()
	cfg.CurseForge.BaseURL = server.URL
	cfg.CurseForge.APIKey = "test-key"
	cfg.RequestTimeoutSeconds = 5
	api := &Server{}
	project := projectFileContext{ProjectType: "mod", ProjectID: "mod000001", CurseForgeProjectID: "123"}
	cursor := ""
	loaded := 0
	for pageNumber := 0; pageNumber < 3; pageNumber++ {
		request, err := parseProjectFilePageRequest(url.Values{"source": {"curseforge"}, "limit": {"20"}, "cursor": {cursor}}, project)
		if err != nil {
			t.Fatal(err)
		}
		page, err := api.loadCurseForgeProjectFilePage(context.Background(), project, request, cfg)
		if err != nil {
			t.Fatal(err)
		}
		loaded += len(page.Items)
		cursor = page.NextCursor
		if pageNumber < 2 && !page.HasMore {
			t.Fatal("expected another CurseForge page")
		}
	}
	if loaded != 45 || cursor != "" {
		t.Fatalf("loaded=%d cursor=%q", loaded, cursor)
	}
}

func TestProjectFileProviderLimitsFailClosed(t *testing.T) {
	manifest := modrinthProjectFileManifest{ID: "project01", Versions: make([]string, projectFileMaximumModrinthVersions+1)}
	for index := range manifest.Versions {
		manifest.Versions[index] = fmt.Sprintf("v%d", index)
	}
	if err := validateModrinthProjectFileManifest(manifest); err == nil || !strings.Contains(err.Error(), "too many versions") {
		t.Fatalf("unexpected manifest limit error: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 101)))
	}))
	defer server.Close()
	if err := getProviderJSONLimited(context.Background(), server.Client(), server.URL, http.Header{}, 100, &map[string]any{}); err == nil {
		t.Fatal("expected oversized provider JSON to fail")
	}
}
