package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProjectAutomationModrinthReleaseUsesProviderContract(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("unexpected provider credential")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"fixture-release","name":"Human display name","version_number":"2.4.1","version_type":"beta","changelog":"Fixture body","date_published":"2025-03-02T08:05:00Z","game_versions":["1.21.1"],"loaders":["fabric"],"files":[{"url":"https://example.invalid/fixture.jar","filename":"fixture.jar","size":3,"hashes":{"sha1":"abc"}}]}]`))
	}))
	defer provider.Close()
	cfg := defaultModImportConfig()
	cfg.Modrinth.BaseURL = provider.URL
	cfg.Modrinth.Token = ""
	cfg.RequestTimeoutSeconds = 2
	releases, err := (&ProjectAutomationWorker{}).loadReleases(context.Background(), projectAutomationJob{SourceType: "modrinth", ExternalID: "fixture"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 || releases[0].Version != "2.4.1" || !releases[0].PublishedAt.Equal(time.Date(2025, 3, 2, 8, 5, 0, 0, time.UTC)) || len(releases[0].Files) != 1 || releases[0].Files[0].ReleaseChannel != "beta" {
		t.Fatalf("provider snake_case contract lost: %#v", releases)
	}
}

func TestProjectAutomationCurseForgeChangelogFailureCannotBecomeFallback(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "controlled-fixture-key" {
			t.Error("expected only the synthetic fixture credential")
		}
		if strings.HasSuffix(r.URL.Path, "/changelog") {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":123,"displayName":"Fixture","fileName":"fixture.jar","releaseType":1,"fileDate":"2025-03-02T08:05:00Z","fileLength":3,"downloadUrl":"https://example.invalid/fixture.jar","gameVersions":["1.21.1","Fabric"]}],"pagination":{"resultCount":1,"totalCount":1}}`))
	}))
	defer provider.Close()
	cfg := defaultModImportConfig()
	cfg.CurseForge.BaseURL = provider.URL
	cfg.CurseForge.APIKey = "controlled-fixture-key"
	cfg.RequestTimeoutSeconds = 2
	releases, err := (&ProjectAutomationWorker{}).loadReleases(context.Background(), projectAutomationJob{SourceType: "curseforge", ExternalID: "123"}, cfg)
	if err == nil || !strings.Contains(err.Error(), "HTTP 503") || len(releases) != 0 {
		t.Fatalf("failed provider body treated as successful empty changelog: releases=%#v err=%v", releases, err)
	}
}
