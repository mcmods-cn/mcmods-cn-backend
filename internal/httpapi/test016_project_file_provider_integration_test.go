package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func test016ProviderVersion(id, projectID, hash string) map[string]any {
	return map[string]any{"id": id, "project_id": projectID, "name": "TEST016 External", "version_number": "1.0", "version_type": "release", "game_versions": []string{"1.21.1"}, "loaders": []string{"neoforge"}, "date_published": "2026-09-30T00:00:00Z", "downloads": 17, "files": []map[string]any{{"filename": "external.jar", "url": "https://cdn.example.invalid/external.jar", "size": 174, "primary": true, "hashes": map[string]string{"sha1": hash, "sha512": strings.Repeat("b", 128)}}}}
}

func (f test016FileFixture) modrinthConfig(t *testing.T, endpoint, projectID string) modImportConfig {
	t.Helper()
	cfg := defaultModImportConfig()
	for _, provider := range []*modImportProviderConfig{&cfg.Modrinth, &cfg.CurseForge, &cfg.GitHub} {
		provider.Token, provider.APIKey = "", ""
		provider.Enabled = false
	}
	cfg.Modrinth.BaseURL, cfg.Modrinth.Enabled = endpoint, true
	cfg.RequestTimeoutSeconds = 5
	sealed, err := f.server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, sealed); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `update mods set modrinth_project_id=$2 where id=$1`, f.modID, projectID); err != nil {
		t.Fatal(err)
	}
	return cfg
}

type test016ProviderList struct {
	Items      []projectFileItem `json:"items"`
	Warnings   map[string]string `json:"warnings"`
	HasMore    bool              `json:"hasMore"`
	NextCursor string            `json:"nextCursor"`
}

func (f test016FileFixture) providerList(t *testing.T, base string) test016ProviderList {
	t.Helper()
	raw := f.require(t, "", http.MethodGet, base+"?source=modrinth&limit=1", nil, http.StatusOK)
	var response struct {
		Data test016ProviderList `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if len(raw) > 16<<10 || strings.Contains(string(raw), "directUrl") || strings.Contains(string(raw), "cdn.example.invalid") {
		t.Fatalf("provider list leaked direct URL or exceeded bounded response: %s", raw)
	}
	return response.Data
}

func TestTEST016ModrinthIdentityLimitsAndFailureDegradationHTTPIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	base := "/api/v1/projects/mod/" + f.modCode + "/files"
	hash := strings.Repeat("a", 40)
	for _, mode := range []string{"valid", "wrong-project", "unrequested-version", "duplicate-version", "http-error", "invalid-json", "oversized-body", "too-many-versions", "duplicate-manifest", "too-many-files", "empty-files", "invalid-download-url"} {
		t.Run(mode, func(t *testing.T) {
			var credentials, calls atomic.Int64
			projectID := "owned-" + mode
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" {
					credentials.Add(1)
				}
				w.Header().Set("Content-Type", "application/json")
				if mode == "http-error" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if mode == "invalid-json" {
					_, _ = w.Write([]byte(`{"broken":`))
					return
				}
				if mode == "oversized-body" {
					_, _ = w.Write([]byte(strings.Repeat("x", int(projectFileProviderJSONLimit)+1)))
					return
				}
				if strings.HasPrefix(r.URL.Path, "/project/") {
					versions := []string{"v1"}
					if mode == "duplicate-version" {
						versions = append(versions, "v2")
					}
					if mode == "duplicate-manifest" {
						versions = append(versions, "v1")
					}
					if mode == "too-many-versions" {
						versions = make([]string, projectFileMaximumModrinthVersions+1)
						for i := range versions {
							versions[i] = fmt.Sprint("v", i)
						}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"id": projectID, "versions": versions})
					return
				}
				version := test016ProviderVersion("v1", projectID, hash)
				if mode == "wrong-project" {
					version["project_id"] = "foreign-project"
				}
				if mode == "unrequested-version" {
					version["id"] = "foreign-version"
				}
				if mode == "empty-files" {
					version["files"] = []map[string]any{}
				}
				if mode == "invalid-download-url" {
					version["files"] = []map[string]any{{"filename": "foreign.jar", "url": "http://127.0.0.1/private", "hashes": map[string]string{"sha1": hash}}}
				}
				if mode == "too-many-files" {
					files := make([]map[string]any, projectFileMaximumFilesPerVersion+1)
					for i := range files {
						files[i] = map[string]any{"url": "https://cdn.example.invalid/" + fmt.Sprint(i), "hashes": map[string]string{"sha1": fmt.Sprint(i)}}
					}
					version["files"] = files
				}
				if strings.HasPrefix(r.URL.Path, "/version_file/") {
					_ = json.NewEncoder(w).Encode(version)
					return
				}
				versions := []map[string]any{version}
				if mode == "duplicate-version" {
					versions = append(versions, version)
				}
				_ = json.NewEncoder(w).Encode(versions)
			}))
			defer provider.Close()
			f.modrinthConfig(t, provider.URL, projectID)
			list := f.providerList(t, base)
			if mode == "valid" {
				if len(list.Items) != 1 || list.Items[0].ID != hash || list.Items[0].Source != "modrinth" || list.Items[0].DownloadCount != 17 || list.Items[0].DownloadCountSource != "modrinth" {
					t.Fatalf("valid external metadata lost: %+v", list)
				}
				raw := f.require(t, "", http.MethodPost, base+"/modrinth/"+hash+"/download", nil, http.StatusOK)
				if !strings.Contains(string(raw), "https://cdn.example.invalid/external.jar") {
					t.Fatalf("validated download URL missing: %s", raw)
				}
			} else if mode == "empty-files" || mode == "invalid-download-url" {
				if len(list.Items) != 0 || list.HasMore || list.NextCursor != "" {
					t.Fatalf("unusable version did not terminate: %+v", list)
				}
			} else {
				if len(list.Items) != 0 || list.Warnings["modrinth"] == "" {
					t.Fatalf("invalid provider response did not fail closed with warning: %+v", list)
				}
				if mode == "wrong-project" || mode == "unrequested-version" {
					f.require(t, "", http.MethodPost, base+"/modrinth/"+hash+"/download", nil, http.StatusNotFound)
				}
			}
			if calls.Load() == 0 || credentials.Load() != 0 {
				t.Fatalf("owned provider was not used or received credentials: calls%d credentials%d", calls.Load(), credentials.Load())
			}
		})
	}
}

func TestTEST016ProviderCacheSeparatesOriginsAndCaseSensitiveProjectIDsHTTPIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	base := "/api/v1/projects/mod/" + f.modCode + "/files"
	makeProvider := func(marker string) *httptest.Server {
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			projectID := "owned"
			if strings.HasPrefix(r.URL.Path, "/project/") {
				projectID = strings.TrimPrefix(r.URL.Path, "/project/")
				_ = json.NewEncoder(w).Encode(map[string]any{"id": projectID, "versions": []string{projectID + "-v1"}})
				return
			}
			var ids []string
			if err := json.Unmarshal([]byte(r.URL.Query().Get("ids")), &ids); err != nil || len(ids) != 1 {
				w.WriteHeader(400)
				return
			}
			projectID = strings.TrimSuffix(ids[0], "-v1")
			hash := strings.Repeat(marker, 40)
			if projectID == "Owned" {
				hash = strings.Repeat("c", 40)
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{test016ProviderVersion(ids[0], projectID, hash)})
		}))
		t.Cleanup(provider.Close)
		return provider
	}
	first, second := makeProvider("a"), makeProvider("d")
	for _, item := range []struct{ endpoint, project, hash string }{{first.URL, "owned", strings.Repeat("a", 40)}, {second.URL, "owned", strings.Repeat("d", 40)}, {second.URL, "Owned", strings.Repeat("c", 40)}, {second.URL, "owned", strings.Repeat("d", 40)}} {
		f.modrinthConfig(t, item.endpoint, item.project)
		list := f.providerList(t, base)
		if len(list.Items) != 1 || list.Items[0].ID != item.hash {
			t.Fatalf("cache reused a different provider origin/project identity: %+v want %s %s %s", list, item.endpoint, item.project, item.hash)
		}
	}
}

func TestTEST016CurseForgeAdapterRejectsForeignProjectFileIdentity(t *testing.T) {
	for _, mode := range []string{"valid", "foreign-project", "wrong-file"} {
		t.Run(mode, func(t *testing.T) {
			var credentials atomic.Int64
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("x-api-key") != "" || r.Header.Get("Authorization") != "" {
					credentials.Add(1)
				}
				projectID, fileID := 123, 7
				if mode == "foreign-project" {
					projectID = 999
				}
				if mode == "wrong-file" {
					fileID = 8
				}
				file := map[string]any{"id": fileID, "modId": projectID, "fileName": "external.jar", "downloadUrl": "https://cdn.example.invalid/external.jar"}
				if r.URL.Path == "/mods/123/files/7" {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": file})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{file}, "pagination": map[string]int{"index": 0, "pageSize": 2, "resultCount": 1, "totalCount": 1}})
			}))
			defer provider.Close()
			cfg := defaultModImportConfig()
			cfg.CurseForge.BaseURL, cfg.CurseForge.APIKey = provider.URL, "test016-synthetic-never-send"
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			file, err := loadCurseForgeProjectFileByID(ctx, "123", "7", cfg)
			if mode == "valid" {
				if err != nil || file.ID != "7" {
					t.Fatalf("valid CF adapter failed: %+v %v", file, err)
				}
			} else if err == nil {
				t.Fatalf("CF adapter accepted foreign identity: %+v", file)
			}
			if mode != "wrong-file" {
				api := &Server{}
				page, err := api.loadCurseForgeProjectFilePage(ctx, projectFileContext{ProjectType: "mod", ProjectID: "mod000001", CurseForgeProjectID: "123"}, projectFilePageRequest{Source: "curseforge", Limit: 1, Scope: "owned"}, cfg)
				if mode == "valid" && (err != nil || len(page.Items) != 1) || mode == "foreign-project" && err == nil {
					t.Fatalf("CF page ownership mismatch: %+v %v", page, err)
				}
			}
			if credentials.Load() != 0 {
				t.Fatal("custom origin received CF credential")
			}
		})
	}
}

func TestTEST016CurseForgeFullHTTPFailsClosedForMissingOrWrongOriginCredentials(t *testing.T) {
	f := newTEST016FileFixture(t)
	base := "/api/v1/projects/mod/" + f.modCode + "/files"
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer provider.Close()
	cfg := f.modrinthConfig(t, provider.URL, "")
	if _, err := f.db.Exec(f.ctx, `update mods set curseforge_project_id='123' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	raw := f.require(t, "", http.MethodGet, base+"?source=curseforge", nil, http.StatusOK)
	if !strings.Contains(string(raw), "CurseForge API key is not configured") {
		t.Fatalf("missing CF credential did not degrade explicitly: %s", raw)
	}
	f.require(t, "", http.MethodPost, base+"/curseforge/7/download", nil, http.StatusNotFound)
	cfg.CurseForge.BaseURL, cfg.CurseForge.APIKey, cfg.CurseForge.Enabled = provider.URL, "test016-synthetic-never-send", true
	sealed, err := f.server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `update system_settings set value=$2::jsonb where key=$1`, modImportConfigSettingKey, sealed); err != nil {
		t.Fatal(err)
	}
	raw = f.require(t, "", http.MethodGet, base+"?source=curseforge", nil, http.StatusOK)
	if !strings.Contains(string(raw), "external download sources are temporarily unavailable") || strings.Contains(string(raw), cfg.CurseForge.APIKey) {
		t.Fatalf("invalid credential origin was not safely rejected: %s", raw)
	}
	f.require(t, "", http.MethodPost, base+"/curseforge/7/download", nil, http.StatusServiceUnavailable)
	if calls.Load() != 0 {
		t.Fatal("full HTTP CF path contacted an untrusted credential origin")
	}
}
