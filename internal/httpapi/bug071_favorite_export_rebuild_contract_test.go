package httpapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestFavoriteExportHistoryReturnsRebuildChoicesAndVersion(t *testing.T) {
	raw, err := json.Marshal(favoriteModpackExportSummary{AllowCompatibleOnly: true, ReportVersion: 7})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"allowCompatibleOnly":true`) || !strings.Contains(text, `"reportVersion":7`) {
		t.Fatalf("history summary omitted rebuild facts: %s", text)
	}
	query, _ := favoriteExportPageSQL(favoriteExportPageRequest{OwnerUserID: 1, Status: "all", Limit: 30})
	if !strings.Contains(query, "allow_compatible_only") || !strings.Contains(query, "report_version") {
		t.Fatalf("history SQL omitted rebuild facts: %s", query)
	}
}

func TestFavoriteExportRebuildRoutesAndSnapshotModesAreExplicit(t *testing.T) {
	serverSource, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(serverSource), "POST /api/v1/users/me/modpack-exports/{taskId}/rebuild-preflight") ||
		!strings.Contains(string(serverSource), "rebuildFavoriteModpackExportPreview") {
		t.Fatal("favorite export rebuild preflight route is missing")
	}
	previewSource, err := os.ReadFile("favorite_modpack_export_preview_snapshot.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(previewSource)
	for _, fragment := range []string{"collection_public_id_snapshot", "source_mode", "coalesce(collection_id,0)", "nullableFavoriteCollectionID"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("detached rebuild preview contract is missing %q", fragment)
		}
	}
}
