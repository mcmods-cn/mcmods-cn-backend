package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestFavoriteExportCollectionDeletionHasAReportRetentionBoundary(t *testing.T) {
	handlers, err := os.ReadFile("favorite_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	deleteBody := goFunctionBody(t, string(handlers), "deleteFavoriteCollection")
	for _, required := range []string{
		"SOURCE_COLLECTION_DELETED", "status in ('pending','processing')", "status='cancelled'", "lease_token=''",
	} {
		if !strings.Contains(deleteBody, required) {
			t.Errorf("collection deletion lifecycle is missing %q", required)
		}
	}
	worker, err := os.ReadFile("favorite_modpack_export_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(worker), "cleanupCancelledFavoriteExportArtifact") {
		t.Fatal("worker does not clean a generated artifact when collection cancellation wins the completion race")
	}
	queries, err := os.ReadFile("favorite_modpack_export_query.go")
	if err != nil {
		t.Fatal(err)
	}
	pages, err := os.ReadFile("favorite_modpack_export_pagination.go")
	if err != nil {
		t.Fatal(err)
	}
	for filename, source := range map[string]string{"query": string(queries), "page": string(pages)} {
		if !strings.Contains(source, "collection_public_id_snapshot") || strings.Contains(source, "join favorite_collections collection") {
			t.Errorf("%s still requires the source collection for report reads", filename)
		}
	}
}
