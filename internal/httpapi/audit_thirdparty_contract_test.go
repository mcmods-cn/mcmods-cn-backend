package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/catalogpolicy"
)

func TestTP001ModrinthPublicSourceGatePrecedesPrivateSubrequests(t *testing.T) {
	for _, status := range []string{"private", "draft", "rejected", "processing", "unknown", "", "approved", "archived", "unlisted"} {
		t.Run(status, func(t *testing.T) {
			secondary := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/project/source" {
					_ = json.NewEncoder(w).Encode(map[string]any{"id": "source", "project_type": "mod", "status": status, "body": "SYNTHETIC_PRIVATE_DOCUMENT", "team": "team"})
				} else {
					secondary++
					_, _ = io.WriteString(w, "[]")
				}
			}))
			defer server.Close()
			cfg := defaultModImportConfig()
			cfg.Modrinth.BaseURL = server.URL
			snapshot, err := loadModrinthProviderSnapshot(context.Background(), server.Client(), cfg, "source")
			public := status == "approved" || status == "archived" || status == "unlisted"
			if public {
				if err != nil || snapshot.Project.ID != "source" || secondary != 2 {
					t.Fatalf("public source: err=%v secondary=%d", err, secondary)
				}
			} else if err == nil || secondary != 0 || snapshot.Project.Body != "" || strings.Contains(err.Error(), "SYNTHETIC_PRIVATE_DOCUMENT") {
				t.Fatalf("private source escaped gate: snapshot=%#v secondary=%d err=%v", snapshot, secondary, err)
			}
		})
	}
}

func TestTP001GitHubRequiresExplicitPublicBeforeReadme(t *testing.T) {
	for _, privacy := range []string{`"private":true,`, ``, `"private":null,`, `"private":false,`} {
		t.Run(privacy, func(t *testing.T) {
			reads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/readme") {
					reads++
					_, _ = io.WriteString(w, "SYNTHETIC_PRIVATE_DOCUMENT")
					return
				}
				_, _ = io.WriteString(w, `{`+privacy+`"name":"source","full_name":"owner/source","owner":{"login":"owner"}}`)
			}))
			defer server.Close()
			cfg := defaultModImportConfig()
			cfg.GitHub.BaseURL = server.URL
			result, err := importGitHubRepository(context.Background(), server.Client(), cfg, "owner/source")
			if privacy == `"private":false,` {
				if err != nil || reads != 1 || result.BodyMarkdown != "SYNTHETIC_PRIVATE_DOCUMENT" {
					t.Fatalf("public import: reads=%d err=%v", reads, err)
				}
			} else if err == nil || reads != 0 || result.BodyMarkdown != "" {
				t.Fatalf("private import: reads=%d result=%#v err=%v", reads, result, err)
			}
		})
	}
}

func TestTP001SourcePublicityIsRecheckedAfterRevocation(t *testing.T) {
	status := "approved"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
	}))
	defer server.Close()
	cfg := defaultModImportConfig()
	cfg.Modrinth.BaseURL = server.URL
	if err := verifyPublicImportSource(context.Background(), cfg, "mod", "modrinth", "https://modrinth.com/mod/source"); err != nil {
		t.Fatal(err)
	}
	status = "private"
	if err := verifyPublicImportSource(context.Background(), cfg, "mod", "modrinth", "https://modrinth.com/mod/source"); err == nil {
		t.Fatal("revoked public source remained readable")
	}
}

func TestTP001ProviderErrorsNeverPersistResponseDocuments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = io.WriteString(w, "SYNTHETIC_PRIVATE_DOCUMENT")
	}))
	defer server.Close()
	_, err := getProviderBytes(context.Background(), server.Client(), server.URL, publicProviderHeaders("test"))
	if err == nil || strings.Contains(err.Error(), "SYNTHETIC_PRIVATE_DOCUMENT") || !strings.Contains(err.Error(), "403") {
		t.Fatalf("unsafe provider error: %v", err)
	}
}

func TestTP002CompatibilityUsesVersionFactsWithoutCartesianProduct(t *testing.T) {
	versions := []modrinthVersion{
		{Status: "listed", Loaders: []string{"fabric"}, GameVersions: []string{"1.20.1"}},
		{Status: "listed", Loaders: []string{"forge"}, GameVersions: []string{"1.21.1"}},
		{Status: "listed", Loaders: []string{"fabric", "quilt"}, GameVersions: []string{"1.20.1"}},
		{Status: "private", Loaders: []string{"fabric"}, GameVersions: []string{"1.22"}},
		{Status: "listed", Loaders: []string{"unknown"}, GameVersions: []string{"1.22"}},
	}
	want := []modLoaderCompatibilityPayload{{Loader: "Fabric", Versions: []string{"1.20.1"}}, {Loader: "Forge", Versions: []string{"1.21.1"}}, {Loader: "Quilt", Versions: []string{"1.20.1"}}}
	if got := modrinthVersionCompatibilities(versions); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	if got := modrinthVersionCompatibilities(nil); len(got) != 0 {
		t.Fatalf("invented facts: %#v", got)
	}
}

func TestTP003CurseForgePaginationFindsSecondPageAndRejectsFailures(t *testing.T) {
	for _, mode := range []string{"second-page", "none", "failure", "repeat", "budget"} {
		t.Run(mode, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				index, _ := strconv.Atoi(r.URL.Query().Get("index"))
				if mode == "failure" && index > 0 {
					w.WriteHeader(429)
					return
				}
				files := make([]curseForgePackFile, 50)
				for i := range files {
					files[i].ID = int64(index + i + 1)
				}
				total := 51
				if mode == "budget" {
					total = 1001
				}
				if mode == "repeat" {
					files[0].ID = 1
				}
				if index >= 50 && mode != "budget" && mode != "repeat" {
					files = []curseForgePackFile{{ID: 51, ReleaseType: 1, FileStatus: 4, IsAvailable: true, FileName: "source.zip", FileDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}
					if mode == "none" {
						files[0].ReleaseType = 2
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": files, "pagination": map[string]int{"index": index, "resultCount": len(files), "totalCount": total}})
			}))
			defer server.Close()
			cfg := defaultModImportConfig()
			cfg.CurseForge.BaseURL = server.URL
			file, err := loadCurseForgePackFile(context.Background(), server.Client(), cfg, 1, publicProviderHeaders("test"))
			if mode == "second-page" {
				if err != nil || file.ID != 51 || requests != 2 {
					t.Fatalf("file=%#v requests=%d err=%v", file, requests, err)
				}
				return
			}
			expected := map[string]string{"none": "没有已批准", "failure": "pagination failed", "repeat": "repeated", "budget": "budget exhausted"}[mode]
			if err == nil || !strings.Contains(err.Error(), expected) || requests > 20 {
				t.Fatalf("requests=%d err=%v want=%q", requests, err, expected)
			}
		})
	}
}

func TestTP004CurseForgeManifestDiscriminatorAndReferences(t *testing.T) {
	var valid curseForgePackManifest
	if err := json.Unmarshal([]byte(`{"manifestType":"minecraftModpack","manifestVersion":1,"minecraft":{"version":"1.20.1","modLoaders":[{"id":"forge-47.4.0"}]},"files":[{"projectID":1,"fileID":2}]}`), &valid); err != nil {
		t.Fatal(err)
	}
	if err := validateCurseForgePackManifest(valid); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(*curseForgePackManifest){func(m *curseForgePackManifest) { m.ManifestType = "other" }, func(m *curseForgePackManifest) { m.ManifestVersion = 0 }, func(m *curseForgePackManifest) { m.ManifestVersion = -1 }, func(m *curseForgePackManifest) { m.ManifestVersion = 2 }, func(m *curseForgePackManifest) { m.Minecraft.Version = "" }, func(m *curseForgePackManifest) { m.Minecraft.ModLoaders = nil }, func(m *curseForgePackManifest) {
		m.Files = nil
		m.Files = append(m.Files, struct {
			ProjectID int64 `json:"projectID"`
			FileID    int64 `json:"fileID"`
			Required  bool  `json:"required"`
		}{})
	}} {
		copy := valid
		mutation(&copy)
		if err := validateCurseForgePackManifest(copy); err == nil {
			t.Fatalf("invalid manifest accepted: %#v", copy)
		}
	}
}

func TestTP005ModpackCategoryMappingUsesSupportedTaxonomy(t *testing.T) {
	for _, value := range []string{"quests", "skyblock", "tech", "optimization", "building", "unknown"} {
		got := primaryModpackCategoryFromExternal([]string{value})
		if !catalogpolicy.IsModpackCategory(got) {
			t.Fatalf("%s mapped outside taxonomy: %s", value, got)
		}
	}
	if got := primaryModpackCategoryFromExternal([]string{"quests"}); got != "quests" {
		t.Fatalf("quests became %s", got)
	}
}

func TestTP006DownloadCannotGrantItselfLoopbackOrPrivateTrust(t *testing.T) {
	hits := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; _, _ = io.WriteString(w, "private") }))
	defer server.Close()
	for _, raw := range []string{server.URL, "https://[::1]/x", "https://[::ffff:127.0.0.1]/x", "https://10.0.0.1/x", "https://169.254.169.254/x", "https://cdn.modrinth.com:444/x", "https://user:pass@cdn.modrinth.com/x", "http://cdn.modrinth.com/x"} {
		if _, err := newProviderDownloadHTTPClient(time.Second, raw); err == nil {
			t.Errorf("untrusted target accepted: %s", raw)
		}
	}
	if hits != 0 {
		t.Fatalf("private target was contacted %d times", hits)
	}
}

func TestDGR003GitHubCDNRedirectIsBoundedAndStripsCredentials(t *testing.T) {
	for _, location := range []string{"https://release-assets.githubusercontent.com/path", "https://objects.githubusercontent.com/path", "https://attacker.example/path", "http://release-assets.githubusercontent.com/path", "https://127.0.0.1/path", "https://cdn.modrinth.com/path"} {
		t.Run(location, func(t *testing.T) {
			client, err := newProviderDownloadHTTPClient(time.Second, "https://github.com/org/repo/releases/download/v1/a.jar")
			if err != nil {
				t.Fatal(err)
			}
			transport := &providerOriginScriptTransport{location: location}
			client.Transport = transport
			request, _ := http.NewRequest(http.MethodGet, "https://github.com/org/repo/releases/download/v1/a.jar", nil)
			request.Header.Set("Authorization", "synthetic")
			request.Header.Set("x-api-key", "synthetic")
			request.Header.Set("Cookie", "synthetic")
			response, err := client.Do(request)
			if response != nil {
				response.Body.Close()
			}
			allowed := strings.HasPrefix(location, "https://release-assets.githubusercontent.com/") || strings.HasPrefix(location, "https://objects.githubusercontent.com/")
			if allowed {
				if err != nil || len(transport.requests) != 2 {
					t.Fatalf("allowed CDN rejected: %v", err)
				}
				if h := transport.requests[1].Header; h.Get("Authorization") != "" || h.Get("x-api-key") != "" || h.Get("Cookie") != "" {
					t.Fatal("credentials crossed download redirect")
				}
			} else if err == nil || len(transport.requests) != 1 {
				t.Fatalf("unsafe redirect escaped: requests=%d err=%v", len(transport.requests), err)
			}
		})
	}
}

func TestTP008ImportedSimpleProjectCollectionsAreArrays(t *testing.T) {
	for projectType, spec := range projectImportSourceSpecs {
		if !simpleProjectTypes[projectType] {
			continue
		}
		for provider, enabled := range map[string]bool{"modrinth": len(spec.ModrinthSections) > 0, "curseforge": len(spec.CurseForgeSections) > 0} {
			if !enabled {
				continue
			}
			t.Run(fmt.Sprintf("%s/%s", projectType, provider), func(t *testing.T) {
				data := simpleProjectImportData{ProjectType: projectType, Provider: provider, ProviderProjectID: "1", Name: "Example", Slug: "example", MinecraftVersions: []string{"1.20.1"}, StructuredValues: []string{"paper", "iris"}, Status: "approved", License: "MIT", ProviderURL: "https://example.com/source"}
				draft := simpleProjectDraftFromExternal(data)
				if err := normalizeAndValidateSimpleProjectDraft(&draft, true); err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(draft)
				if err != nil {
					t.Fatal(err)
				}
				var object map[string]any
				if err = json.Unmarshal(raw, &object); err != nil {
					t.Fatal(err)
				}
				for _, field := range []string{"localizations", "minecraftVersions", "loaders", "categories", "features", "searchKeywords", "authors", "links", "galleryImages", "parentProjects"} {
					if _, ok := object[field].([]any); !ok {
						t.Fatalf("%s is not an array: %s", field, raw)
					}
				}
			})
		}
	}
}

func TestTP006ConfiguredProviderOriginDoesNotAuthorizeOtherLocalTargets(t *testing.T) {
	base := "https://127.0.0.1:43123/api"
	client, err := newProviderDownloadHTTPClient(time.Second, "https://127.0.0.1:43123/archive.zip", base)
	if err != nil {
		t.Fatal("explicit same-origin fixture rejected:", err)
	}
	transport := &providerOriginScriptTransport{location: "https://127.0.0.1:43124/private"}
	client.Transport = transport
	request, _ := http.NewRequest(http.MethodGet, "https://127.0.0.1:43123/archive.zip", nil)
	response, err := client.Do(request)
	if response != nil {
		response.Body.Close()
	}
	if err == nil || len(transport.requests) != 1 {
		t.Fatalf("configured origin authorized unrelated target: requests=%d err=%v", len(transport.requests), err)
	}
	if _, err = newProviderDownloadHTTPClient(time.Second, "https://127.0.0.1:43124/private", base); err == nil {
		t.Fatal("untrusted artifact self-authorized a different loopback origin")
	}
}

func TestTP008LegacyResultCollectionBoundaryPreservesNonemptyValuesAndExtensions(t *testing.T) {
	raw := []byte(`{"galleryImages":null,"parentProjects":[{"type":"mod","id":"abc123456"}],"authors":[{"name":"Example"}],"customExtension":{"preserve":true}}`)
	normalized, err := normalizeSimpleProjectImportResultCollections(raw)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if err = json.Unmarshal(normalized, &result); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"galleryImages", "localizations", "minecraftVersions", "loaders", "categories", "features", "searchKeywords", "links"} {
		if string(result[key]) != "[]" {
			t.Fatalf("%s did not normalize: %s", key, normalized)
		}
	}
	if !strings.Contains(string(result["parentProjects"]), "abc123456") || !strings.Contains(string(result["authors"]), "Example") || string(result["customExtension"]) != `{"preserve":true}` {
		t.Fatalf("nonempty or extension data lost: %s", normalized)
	}
	if _, err = normalizeSimpleProjectImportResultCollections([]byte(`{"galleryImages":{}}`)); err == nil {
		t.Fatal("non-array collection silently accepted")
	}
}

func TestTP002ModImportUsesActualHTTPVersionPairs(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/project/example":
			_, _ = io.WriteString(w, `{"id":"example","slug":"example","title":"Example","project_type":"mod","status":"approved","loaders":["fabric","forge"],"game_versions":["1.20.1","1.21.1"],"license":{"id":"MIT"}}`)
		case "/project/example/version":
			_, _ = io.WriteString(w, `[{"status":"listed","loaders":["fabric"],"game_versions":["1.20.1"]},{"status":"listed","loaders":["forge"],"game_versions":["1.21.1"]}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	cfg := defaultModImportConfig()
	cfg.Modrinth.BaseURL = provider.URL
	draft, err := importModrinthProject(context.Background(), provider.Client(), cfg, "example")
	if err != nil {
		t.Fatal(err)
	}
	want := []modLoaderCompatibilityPayload{{Loader: "Fabric", Versions: []string{"1.20.1"}}, {Loader: "Forge", Versions: []string{"1.21.1"}}}
	if !reflect.DeepEqual(draft.Compatibilities, want) {
		t.Fatalf("HTTP import invented relations: %#v", draft.Compatibilities)
	}
}
