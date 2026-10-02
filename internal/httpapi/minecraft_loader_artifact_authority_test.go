package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMinecraftVersionCatalogHashTracksOnlyMRPackCompatibility(t *testing.T) {
	base := minecraftVersionConfig{
		Versions:       []minecraftVersionOption{{Code: "1.21.1", Type: "release"}, {Code: "1.20.1", Type: "release"}},
		CommonVersions: []string{"1.21.1"},
		Loaders: []minecraftLoaderOption{
			{Code: "Forge", Name: "Forge", Versions: []string{"1.21.1", "1.20.1"}},
			{Code: "LiteLoader", Name: "LiteLoader", Versions: []string{"1.20.1"}},
		},
	}
	presentationOnly := minecraftVersionConfig{
		Versions:       []minecraftVersionOption{{Code: "1.20.1", Type: "snapshot"}, {Code: "1.21.1", Type: "snapshot"}},
		CommonVersions: []string{"1.20.1"},
		Loaders: []minecraftLoaderOption{
			{Code: "LiteLoader", Name: "Changed", Versions: []string{"1.21.1"}},
			{Code: "Forge", Name: "Changed Forge Name", Versions: []string{"1.20.1", "1.21.1"}},
		},
	}
	baseHash, err := minecraftVersionCatalogHash(base)
	if err != nil {
		t.Fatal(err)
	}
	presentationHash, err := minecraftVersionCatalogHash(presentationOnly)
	if err != nil {
		t.Fatal(err)
	}
	if presentationHash != baseHash {
		t.Fatalf("presentation-only changes altered compatibility hash: %s != %s", presentationHash, baseHash)
	}
	changed := base
	changed.Loaders = []minecraftLoaderOption{{Code: "Forge", Name: "Forge", Versions: []string{"1.20.1"}}}
	changedHash, err := minecraftVersionCatalogHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if changedHash == baseHash {
		t.Fatal("MRPack compatibility change retained the old catalog hash")
	}
}

func TestValidateMinecraftLoaderArtifactSnapshotsRequiresEverySelectableTuple(t *testing.T) {
	config := minecraftVersionConfig{
		Versions: []minecraftVersionOption{{Code: "1.21.1"}, {Code: "1.20.1"}},
		Loaders:  []minecraftLoaderOption{{Code: "Forge", Versions: []string{"1.21.1", "1.20.1"}}},
	}
	_, err := validateMinecraftLoaderArtifactSnapshots(config, []minecraftLoaderArtifactSnapshot{{
		MinecraftVersion: "1.20.1", Loader: "forge", LoaderVersion: "47.4.10",
		SourceURL: forgeMavenMetadataURL, ObservedAt: time.Now(),
	}})
	if err == nil {
		t.Fatal("incomplete loader artifact snapshot was accepted")
	}
}

func TestFavoriteExportMapsLoaderArtifactAuthorityFailures(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "missing tuple", err: errMinecraftLoaderArtifactUnavailable, status: http.StatusUnprocessableEntity, code: "MODPACK_EXPORT_LOADER_SNAPSHOT_UNAVAILABLE"},
		{name: "authority failure", err: errMinecraftLoaderArtifactAuthorityUnavailable, status: http.StatusInternalServerError, code: "MODPACK_EXPORT_CATALOG_UNAVAILABLE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeFavoriteExportError(response, test.err)
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.code) {
				t.Fatalf("response = %d/%s", response.Code, response.Body.String())
			}
		})
	}
}
