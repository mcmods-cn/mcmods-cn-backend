package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func auditResumeMetadataSettings(t *testing.T, f auditFixGRFixture, kind, base string) {
	t.Helper()
	cfg := defaultModImportConfig()
	cfg.Modrinth = modImportProviderConfig{Enabled: kind == "modrinth", BaseURL: cfg.Modrinth.BaseURL}
	cfg.GitHub = modImportProviderConfig{Enabled: kind == "github", BaseURL: cfg.GitHub.BaseURL}
	cfg.CurseForge = modImportProviderConfig{Enabled: false, BaseURL: cfg.CurseForge.BaseURL}
	if kind == "modrinth" {
		cfg.Modrinth.BaseURL = base
	} else {
		cfg.GitHub.BaseURL = base
	}
	sealed, err := f.server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2) on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, sealed); err != nil {
		t.Fatal(err)
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.create")
}
func auditResumeMetadataJob(t *testing.T, f auditFixGRFixture, provider string) modMetadataImportJobResponse {
	t.Helper()
	source := "https://modrinth.com/mod/owned-source"
	if provider == "github" {
		source = "https://github.com/owned/source"
	}
	job := decodeTEST022Data[modMetadataImportJobResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/mod-imports", modMetadataImportRequest{Provider: provider, URL: source}, 202))
	if job.Status != "queued" || job.ID == "" {
		t.Fatal("normal import API did not create a queued actor-owned job")
	}
	return job
}
func auditResumePublicModJSON(t *testing.T, w http.ResponseWriter, status string) {
	t.Helper()
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "owned-source", "slug": "owned-source", "title": "Owned source Mod", "description": "Synthetic metadata producer", "body": "SYNTHETIC_PROVIDER_DOCUMENT", "project_type": "mod", "status": status, "loaders": []string{"fabric", "forge"}, "game_versions": []string{"1.99"}, "client_side": "required", "server_side": "required", "categories": []string{"utility"}, "license": map[string]string{"id": "MIT"}})
}

// The normal provider configuration and HTTP middleware are unchanged. The
// explicit public-source TLS fixture has no upstream credentials. Separate
// source-gate unit tests cover shared credential headers; no real grant or
// official provider transport is inferred from this controlled provider.
func TestAuditResumeTP001ActualWorkerNonpublicSourceHasNoResultOrPublicationIntegration(t *testing.T) {
	for _, tc := range []struct{ provider, state string }{{"modrinth", "private"}, {"modrinth", "draft"}, {"modrinth", "rejected"}, {"modrinth", "unknown"}, {"modrinth", ""}, {"github", "true"}, {"github", "null"}, {"github", "missing"}} {
		t.Run(tc.provider+"/"+tc.state, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			var secondary atomic.Int32
			provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" {
					t.Error("uncredentialed local source got a credential")
				}
				if tc.provider == "modrinth" && r.URL.Path == "/project/owned-source" {
					auditResumePublicModJSON(t, w, tc.state)
					return
				}
				if tc.provider == "github" && r.URL.Path == "/repos/owned/source" {
					body := map[string]any{"name": "source", "full_name": "owned/source", "description": "SYNTHETIC_PROVIDER_DOCUMENT", "owner": map[string]string{"login": "owned"}}
					if tc.state == "true" {
						body["private"] = true
					} else if tc.state == "null" {
						body["private"] = nil
					}
					_ = json.NewEncoder(w).Encode(body)
					return
				}
				secondary.Add(1)
				_, _ = w.Write([]byte(`[]`))
			}))
			t.Cleanup(provider.Close)
			original := http.DefaultTransport
			http.DefaultTransport = provider.Client().Transport
			t.Cleanup(func() { http.DefaultTransport = original })
			auditResumeMetadataSettings(t, f, tc.provider, provider.URL)
			job := auditResumeMetadataJob(t, f, tc.provider)
			before := auditResumeModpackFacts(t, f)
			if err := f.server.runModMetadataImport(f.ctx, job.ID); err != nil {
				t.Fatal(err)
			}
			result := decodeTEST022Data[modMetadataImportJobResponse](t, f.require(t, f.editor, http.MethodGet, "/api/v1/mod-imports/"+job.ID, nil, 200))
			if result.Status != "failed" || result.Progress != 25 || len(result.Result) != 0 || result.Error != "import source is not confirmed public" || secondary.Load() != 0 {
				t.Fatalf("nonpublic job state=%s/%d resultBytes=%d error=%q secondary=%d", result.Status, result.Progress, len(result.Result), result.Error, secondary.Load())
			}
			var nullResult, finished bool
			var persistedError string
			if err := f.db.QueryRow(f.ctx, `select result is null,finished_at is not null,error from mod_metadata_import_jobs where public_id=$1`, job.ID).Scan(&nullResult, &finished, &persistedError); err != nil {
				t.Fatal(err)
			}
			if !nullResult || !finished || strings.Contains(persistedError, "SYNTHETIC_PROVIDER_DOCUMENT") {
				t.Fatal("private response persisted in a failed result/error")
			}
			f.require(t, f.denied, http.MethodGet, "/api/v1/mod-imports/"+job.ID, nil, 403)
			f.require(t, f.otherEditor, http.MethodGet, "/api/v1/mod-imports/"+job.ID, nil, 404)
			f.require(t, "", http.MethodGet, "/api/v1/mod-imports/"+job.ID, nil, 401)
			auditResumeRequireFacts(t, before, auditResumeModpackFacts(t, f))
			t.Logf("TP001_REAL_FAILED_JOB provider=%s sourceState=%q normalAPI202 realWorkerFailed25 resultNULL=true secondary0=true zeroPublicationFacts=true", tc.provider, tc.state)
		})
	}
}

func TestAuditResumeTP001CompletedWorkerRechecksWithdrawalRestoreAndOutageHTTPPostgresIntegration(t *testing.T) {
	for _, kind := range []string{"modrinth", "github"} {
		t.Run(kind, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			var state atomic.Value
			state.Store("public")
			var secondary atomic.Int32
			provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				current := state.Load().(string)
				if current == "outage" {
					http.Error(w, "SYNTHETIC_PROVIDER_DOCUMENT", 503)
					return
				}
				if kind == "modrinth" && r.URL.Path == "/project/owned-source" {
					visibility := "approved"
					if current != "public" {
						visibility = "private"
					}
					auditResumePublicModJSON(t, w, visibility)
					return
				}
				if kind == "github" && r.URL.Path == "/repos/owned/source" {
					_ = json.NewEncoder(w).Encode(map[string]any{"name": "source", "full_name": "owned/source", "private": current != "public", "owner": map[string]string{"login": "owned"}, "license": map[string]string{"spdx_id": "MIT"}})
					return
				}
				secondary.Add(1)
				if kind == "github" && r.URL.Path == "/repos/owned/source/readme" {
					_, _ = w.Write([]byte("SYNTHETIC_PROVIDER_DOCUMENT fabric minecraft"))
					return
				}
				if kind == "modrinth" && r.URL.Path == "/project/owned-source/version" {
					_ = json.NewEncoder(w).Encode([]map[string]any{{"status": "listed", "loaders": []string{"fabric"}, "game_versions": []string{"1.20.1"}}})
					return
				}
				http.NotFound(w, r)
			}))
			t.Cleanup(provider.Close)
			original := http.DefaultTransport
			http.DefaultTransport = provider.Client().Transport
			t.Cleanup(func() { http.DefaultTransport = original })
			auditResumeMetadataSettings(t, f, kind, provider.URL)
			job := auditResumeMetadataJob(t, f, kind)
			if err := f.server.runModMetadataImport(f.ctx, job.ID); err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/mod-imports/" + job.ID
			completed := decodeTEST022Data[modMetadataImportJobResponse](t, f.require(t, f.editor, http.MethodGet, path, nil, 200))
			if completed.Status != "completed" || completed.Progress != 100 || !strings.Contains(string(completed.Result), "SYNTHETIC_PROVIDER_DOCUMENT") || secondary.Load() != 1 {
				t.Fatal("public actual Worker did not yield the lawful saved document")
			}
			var saved string
			if err := f.db.QueryRow(f.ctx, `select to_jsonb(job)::text from mod_metadata_import_jobs job where public_id=$1`, job.ID).Scan(&saved); err != nil {
				t.Fatal(err)
			}
			for _, mode := range []struct {
				state string
				code  int
			}{{"private", 403}, {"outage", 503}, {"public", 200}} {
				state.Store(mode.state)
				before := auditResumeModpackFacts(t, f)
				raw := f.require(t, f.editor, http.MethodGet, path, nil, mode.code)
				if mode.code != 200 && strings.Contains(string(raw), "SYNTHETIC_PROVIDER_DOCUMENT") {
					t.Fatal("withdrawal or outage disclosed stored source bytes")
				}
				auditResumeRequireFacts(t, before, auditResumeModpackFacts(t, f))
			}
			f.require(t, f.denied, http.MethodGet, path, nil, 403)
			f.require(t, f.otherEditor, http.MethodGet, path, nil, 404)
			f.require(t, "", http.MethodGet, path, nil, 401)
			var after string
			if err := f.db.QueryRow(f.ctx, `select to_jsonb(job)::text from mod_metadata_import_jobs job where public_id=$1`, job.ID).Scan(&after); err != nil || after != saved {
				t.Fatal("current source revalidation rewrote completed job")
			}
			t.Logf("TP001_REAL_COMPLETED_JOB provider=%s creator200 cross404 anonymous401 withdrawal403 outage503 restore200 persistedResultUnchanged=true", kind)
		})
	}
}

func TestAuditResumeTP002ActualWorkerKeepsOnlyVersionSupportedPairsHTTPPostgresIntegration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		versions []modrinthVersion
		expected []modLoaderCompatibilityPayload
	}{
		{"interleaved", []modrinthVersion{{Status: "listed", Loaders: []string{"fabric"}, GameVersions: []string{"1.20.1"}}, {Status: "listed", Loaders: []string{"forge"}, GameVersions: []string{"1.19.4"}}}, []modLoaderCompatibilityPayload{{Loader: "Fabric", Versions: []string{"1.20.1"}}, {Loader: "Forge", Versions: []string{"1.19.4"}}}},
		{"multiple-and-duplicate", []modrinthVersion{{Status: "listed", Loaders: []string{"fabric", "quilt", "fabric"}, GameVersions: []string{"1.20.1", "1.20.1"}}, {Status: "unlisted", Loaders: []string{"quilt"}, GameVersions: []string{"1.19.4"}}, {Status: "archived", Loaders: []string{"forge"}, GameVersions: []string{"1.20.1"}}}, []modLoaderCompatibilityPayload{{Loader: "Fabric", Versions: []string{"1.20.1"}}, {Loader: "Forge", Versions: []string{"1.20.1"}}, {Loader: "Quilt", Versions: []string{"1.19.4", "1.20.1"}}}},
		{"empty-records", []modrinthVersion{}, []modLoaderCompatibilityPayload{}},
		{"empty-version-list", []modrinthVersion{{Status: "listed", Loaders: []string{"fabric"}, GameVersions: []string{}}}, []modLoaderCompatibilityPayload{{Loader: "Fabric", Versions: []string{}}}},
		{"unknown-loader", []modrinthVersion{{Status: "listed", Loaders: []string{"future_loader"}, GameVersions: []string{"1.20.1"}}}, []modLoaderCompatibilityPayload{}},
		{"private-version-not-a-fact", []modrinthVersion{{Status: "listed", Loaders: []string{"forge"}, GameVersions: []string{"1.20.1"}}, {Status: "private", Loaders: []string{"fabric"}, GameVersions: []string{"1.99"}}}, []modLoaderCompatibilityPayload{{Loader: "Forge", Versions: []string{"1.20.1"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/project/owned-source":
					auditResumePublicModJSON(t, w, "approved")
				case "/project/owned-source/version":
					_ = json.NewEncoder(w).Encode(tc.versions)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(provider.Close)
			original := http.DefaultTransport
			http.DefaultTransport = provider.Client().Transport
			t.Cleanup(func() { http.DefaultTransport = original })
			auditResumeMetadataSettings(t, f, "modrinth", provider.URL)
			job := auditResumeMetadataJob(t, f, "modrinth")
			if err := f.server.runModMetadataImport(f.ctx, job.ID); err != nil {
				t.Fatal(err)
			}
			completed := decodeTEST022Data[modMetadataImportJobResponse](t, f.require(t, f.editor, http.MethodGet, "/api/v1/mod-imports/"+job.ID, nil, 200))
			if completed.Status != "completed" || completed.Progress != 100 {
				t.Fatalf("version facts job state=%s error=%s", completed.Status, completed.Error)
			}
			var draft createModRequest
			if err := json.Unmarshal(completed.Result, &draft); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(draft.Compatibilities, tc.expected) {
				t.Fatalf("actual persisted Worker pairs=%#v expected=%#v", draft.Compatibilities, tc.expected)
			}
			var publicWrites int
			if err := f.db.QueryRow(f.ctx, `select count(*) from mods where modrinth_project_id='owned-source'`).Scan(&publicWrites); err != nil || publicWrites != 0 {
				t.Fatal("metadata preview fabricated a published source Mod")
			}
			t.Logf("TP002_REAL_WORKER case=%s exactPairs=true summaryCartesianIgnored=true noAutomaticPublication=true", tc.name)
		})
	}
}
