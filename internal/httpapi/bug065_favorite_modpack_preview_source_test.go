package httpapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestFavoriteModpackCreationConsumesPreflightSnapshot(t *testing.T) {
	raw, err := os.ReadFile("favorite_modpack_export.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	preflight := goFunctionBody(t, source, "preflightFavoriteModpackExport")
	create := goFunctionBody(t, source, "createFavoriteModpackExport")
	for _, required := range []string{"persistFavoriteModpackExportPreview", "PreviewID", "PreviewHash"} {
		if !strings.Contains(preflight+source, required) {
			t.Errorf("preflight snapshot boundary is missing %q", required)
		}
	}
	for _, required := range []string{"loadFavoriteModpackExportPreviewForCreate", "markFavoriteModpackExportPreviewConsumed"} {
		if !strings.Contains(create, required) {
			t.Errorf("creation snapshot boundary is missing %q", required)
		}
	}
	if strings.Contains(create, "buildFavoriteModpackExportPreview") {
		t.Fatal("creation still rebuilds the live preview after user confirmation")
	}
}

func TestFavoriteModpackPreviewJSONKeepsInternalRoutesPrivate(t *testing.T) {
	routeID := int64(65001)
	preview := favoriteModpackExportPreview{PreviewID: "b065json1", PreviewHash: strings.Repeat("a", 64),
		favoriteModpackExportPreviewSnapshot: favoriteModpackExportPreviewSnapshot{CollectionID: 65,
			CollectionPublicID: "b065col01", MinecraftVersion: "1.21.1", Loader: "neoforge", LoaderVersion: "21.1.100",
			Items: []favoriteModpackExportItem{{SourceProjectRouteID: &routeID, SourceProjectType: "mod", SourceProjectName: "Visible Mod"}}}}
	raw, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	response := string(raw)
	for _, required := range []string{`"previewId":"b065json1"`, `"collectionId":"b065col01"`, `"loaderVersion":"21.1.100"`} {
		if !strings.Contains(response, required) {
			t.Errorf("preview response is missing %s: %s", required, response)
		}
	}
	if strings.Contains(response, "65001") || strings.Contains(response, "sourceProjectRoute") {
		t.Fatalf("preview response leaked an internal route identity: %s", response)
	}
}
