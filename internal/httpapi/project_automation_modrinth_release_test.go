package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func TestProjectAutomationModrinthReleaseUsesProviderMetadata(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/project/fixture-pack/version" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{
			"id": "fixture-release",
			"name": "A friendly release title",
			"version_number": "2.5.0-beta.3",
			"version_type": "beta",
			"date_published": "2025-05-06T12:31:42.123Z",
			"changelog": "Updated the starter recipes.",
			"game_versions": ["1.20.1"],
			"loaders": ["fabric"],
			"files": [{
				"url": "https://cdn.modrinth.com/data/fixture-pack/versions/fixture-release/pack.jar",
				"filename": "pack.jar",
				"size": 42,
				"hashes": {"sha1": "fixture-sha1", "sha512": "fixture-sha512"}
			}]
		}]`))
	}))
	defer provider.Close()

	cfg := defaultModImportConfig()
	cfg.Modrinth.BaseURL = provider.URL
	cfg.Modrinth.Token = ""
	worker := &ProjectAutomationWorker{}
	releases, err := worker.loadReleases(context.Background(), projectAutomationJob{
		SourceType: "modrinth",
		ExternalID: "fixture-pack",
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 {
		t.Fatalf("received %d releases, want 1", len(releases))
	}
	release := releases[0]
	wantPublished := time.Date(2025, time.May, 6, 12, 31, 42, 123_000_000, time.UTC)
	if release.ID != "fixture-release" || release.Version != "2.5.0-beta.3" ||
		!release.PublishedAt.Equal(wantPublished) || release.Body != "Updated the starter recipes." {
		t.Errorf("release identity or metadata changed during import: %+v", release)
	}
	if !latestProviderReleaseActivity(releases).Equal(wantPublished) {
		t.Error("maintenance activity must use the source's release date")
	}
	if !slices.Equal(release.GameVersions, []string{"1.20.1"}) || !slices.Equal(release.Loaders, []string{"Fabric"}) {
		t.Errorf("release compatibility changed during import: versions=%v loaders=%v", release.GameVersions, release.Loaders)
	}
	if len(release.Files) != 1 {
		t.Fatalf("received %d release files, want 1", len(release.Files))
	}
	file := release.Files[0]
	if file.FileName != "pack.jar" || file.VersionName != "2.5.0-beta.3" || file.ReleaseChannel != "beta" ||
		file.SizeBytes != 42 || file.SHA1 != "fixture-sha1" || file.SHA512 != "fixture-sha512" ||
		file.DirectURL != "https://cdn.modrinth.com/data/fixture-pack/versions/fixture-release/pack.jar" {
		t.Errorf("release-file metadata changed during import: %+v", file)
	}
}
