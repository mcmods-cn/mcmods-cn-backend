package httpapi

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestMinecraftLoaderVersionSourcesLive(t *testing.T) {
	if os.Getenv("MCMODS_LIVE_VERSION_SOURCES") != "1" {
		t.Skip("set MCMODS_LIVE_VERSION_SOURCES=1 to verify the remote version sources")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	manifest, _, err := fetchMojangVersionManifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	known := make([]string, 0, len(manifest.Versions))
	for _, version := range manifest.Versions {
		known = append(known, version.ID)
	}
	tests := []struct {
		name     string
		expected string
		fetch    func(context.Context, []string) ([]string, string, bool, error)
	}{
		{name: "Forge", expected: "1.20.1", fetch: fetchForgeMinecraftVersions},
		{name: "NeoForge", expected: "1.21.1", fetch: fetchNeoForgeMinecraftVersions},
		{name: "Fabric", expected: "1.21.1", fetch: fetchFabricMinecraftVersions},
		{name: "LiteLoader", expected: "1.12.2", fetch: fetchLiteLoaderMinecraftVersions},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			versions, sourceURL, _, err := test.fetch(ctx, known)
			if err != nil {
				t.Fatal(err)
			}
			if sourceURL == "" || !containsMinecraftVersion(versions, test.expected) {
				t.Fatalf("source %q returned %d versions without expected %q", sourceURL, len(versions), test.expected)
			}
		})
	}
}

func containsMinecraftVersion(versions []string, expected string) bool {
	for _, version := range versions {
		if version == expected {
			return true
		}
	}
	return false
}
