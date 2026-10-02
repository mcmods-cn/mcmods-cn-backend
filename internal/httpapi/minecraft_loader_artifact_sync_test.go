package httpapi

import (
	"context"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestDecodeCurrentFabricLoaderVersionPrefersNewestStableArtifact(t *testing.T) {
	version, err := decodeCurrentFabricLoaderVersion([]byte(`[
		{"version":"0.16.10","stable":true},
		{"version":"0.17.0-beta.1","stable":false},
		{"version":"0.16.14","stable":true}
	]`))
	if err != nil || version != "0.16.14" {
		t.Fatalf("Fabric loader artifact = %q, %v", version, err)
	}
}

func TestSynchronizedMavenArtifactsRetainOnlyResolvedCatalogVersions(t *testing.T) {
	artifacts := selectSynchronizedMavenLoaderArtifacts([]string{"1.20.1", "1.21.1"}, "forge", forgeMavenMetadataURL,
		timeForMinecraftArtifactTest(), func(minecraftVersion string) (string, error) {
			return selectLoaderArtifactVersion([]string{"1.20.1-47.4.10"}, minecraftVersion+"-")
		})
	if len(artifacts) != 1 || artifacts[0].MinecraftVersion != "1.20.1" || artifacts[0].LoaderVersion != "47.4.10" {
		t.Fatalf("resolved artifacts = %#v", artifacts)
	}
}

func TestSynchronizedNeoForgeArtifactsCoverBothMinecraftReleaseSchemes(t *testing.T) {
	resetMinecraftSourceCacheForTest()
	defer resetMinecraftSourceCacheForTest()
	originalClient := minecraftVersionHTTPClient
	defer func() { minecraftVersionHTTPClient = originalClient }()
	minecraftVersionHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case neoForgeMavenMetadataURL:
			return loaderMetadataResponse(request, http.StatusOK, `<metadata><versioning><versions>
				<version>21.0.167-beta</version><version>21.0.168</version>
				<version>21.1.241</version><version>26.2.0.61</version><version>26.2.0.62-alpha</version>
			</versions></versioning></metadata>`), nil
		case neoForgeLegacyMetadataURL:
			return loaderMetadataResponse(request, http.StatusOK, `<metadata><versioning><versions><version>1.20.1-47.1.106</version></versions></versioning></metadata>`), nil
		default:
			return loaderMetadataResponse(request, http.StatusNotFound, ``), nil
		}
	})}

	artifacts, err := fetchSynchronizedLoaderArtifactVersions(context.Background(), "neoforge", []string{"1.20.1", "1.21", "1.21.1", "26.2"}, timeForMinecraftArtifactTest())
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]string, len(artifacts))
	for _, artifact := range artifacts {
		got[artifact.MinecraftVersion] = artifact.LoaderVersion
	}
	want := map[string]string{
		"1.20.1": "47.1.106",
		"1.21":   "21.0.168",
		"1.21.1": "21.1.241",
		"26.2":   "26.2.0.61",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NeoForge release mappings=%#v; want %#v", got, want)
	}
}

func TestSynchronizeMRPackLoaderArtifactsBuildsBoundedProvenanceSnapshot(t *testing.T) {
	resetMinecraftSourceCacheForTest()
	defer resetMinecraftSourceCacheForTest()
	originalClient := minecraftVersionHTTPClient
	defer func() { minecraftVersionHTTPClient = originalClient }()
	var requests atomic.Int64
	minecraftVersionHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		switch request.URL.String() {
		case fabricLoaderCatalogURL:
			return loaderMetadataResponse(request, http.StatusOK, `[{"version":"0.16.14","stable":true}]`), nil
		case forgeMavenMetadataURL:
			return loaderMetadataResponse(request, http.StatusOK, `<metadata><versioning><versions><version>1.20.1-47.4.10</version></versions></versioning></metadata>`), nil
		case neoForgeLegacyMetadataURL:
			return loaderMetadataResponse(request, http.StatusOK, `<metadata><versioning><versions><version>1.20.1-47.1.106</version></versions></versioning></metadata>`), nil
		default:
			return loaderMetadataResponse(request, http.StatusNotFound, ``), nil
		}
	})}
	config := minecraftVersionConfig{
		Versions:     []minecraftVersionOption{{Code: "1.21.1"}, {Code: "1.20.1"}},
		LastSyncedAt: "2026-08-23T04:00:00Z",
		Loaders: []minecraftLoaderOption{
			{Code: "Fabric", Versions: []string{"1.21.1", "1.20.1"}},
			{Code: "Forge", Versions: []string{"1.21.1", "1.20.1"}},
			{Code: "NeoForge", Versions: []string{"1.20.1"}},
		},
		LoaderSyncs: []minecraftLoaderSyncStatus{
			{Code: "Fabric", Status: "synced", VersionCount: 2},
			{Code: "Forge", Status: "synced", VersionCount: 2},
			{Code: "NeoForge", Status: "synced", VersionCount: 1},
		},
	}
	artifacts, err := synchronizeMRPackLoaderArtifacts(context.Background(), &config)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 3 {
		t.Fatalf("artifact synchronization made %d source requests, want 3 bounded catalogs", requests.Load())
	}
	if len(artifacts) != 4 {
		t.Fatalf("artifact snapshot count = %d, want 4", len(artifacts))
	}
	if !reflect.DeepEqual(config.Loaders[1].Versions, []string{"1.20.1"}) {
		t.Fatalf("Forge selector versions = %#v", config.Loaders[1].Versions)
	}
	if config.LoaderSyncs[1].Status != "failed" || config.LoaderSyncs[1].VersionCount != 1 || config.LoaderSyncs[1].Error == "" {
		t.Fatalf("Forge partial artifact status = %#v", config.LoaderSyncs[1])
	}
	for _, artifact := range artifacts {
		if artifact.SourceURL == "" || artifact.ObservedAt.IsZero() || artifact.LoaderVersion == "" {
			t.Fatalf("artifact has incomplete provenance: %#v", artifact)
		}
	}
}

func timeForMinecraftArtifactTest() time.Time {
	return time.Date(2026, time.August, 23, 4, 0, 0, 0, time.UTC)
}
