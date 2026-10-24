package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/activity"
)

func TestAuditFixCMP003FavoriteExportActivitySharesPreviewAndJobTransaction(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "favorite.modpack_export", "favorite.modpack_export.view_own")
	collection := decodeTEST022Data[favoriteCollectionSummary](t, f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/favorite-collections", map[string]any{"name": "Atomic export collection", "isPublic": false}, 201))
	f.require(t, f.editor, http.MethodPut, "/api/v1/users/me/favorites", map[string]any{"entityType": "mod", "entityPublicId": f.modCode, "collectionIds": []string{collection.ID}}, 200)
	catalog := minecraftVersionConfig{Versions: []minecraftVersionOption{{Code: "1.21.1", Type: "release"}}, CommonVersions: []string{"1.21.1"}, Loaders: []minecraftLoaderOption{{Code: "Fabric", Name: "Fabric", Versions: []string{"1.21.1"}}}}
	if err := saveSynchronizedMinecraftVersionConfig(f.ctx, f.db, catalog, []minecraftLoaderArtifactSnapshot{{MinecraftVersion: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14", SourceURL: fabricLoaderCatalogURL, ObservedAt: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	recover := func(path string, body any, want int) []byte {
		t.Helper()
		prior := auditFixActivityIntentCount(t, f, f.editor, activity.ActionCreate, activity.ObjectUser)
		auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
		before := auditFixFavoriteActivityFacts(t, f)
		response := f.require(t, f.editor, http.MethodPost, path, body, 500)
		auditFixRequireSpecificActivityError(t, response, "MODPACK_EXPORT_ACTIVITY_UNAVAILABLE")
		auditFixRequireProjectFactsUnchanged(t, before, auditFixFavoriteActivityFacts(t, f))
		auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
		raw := f.require(t, f.editor, http.MethodPost, path, body, want)
		if got := auditFixActivityIntentCount(t, f, f.editor, activity.ActionCreate, activity.ObjectUser); got != prior+1 {
			t.Fatalf("export activity=%d prior=%d path=%s", got, prior, path)
		}
		return raw
	}
	base := "/api/v1/users/me/favorite-collections/" + collection.ID + "/modpack-exports"
	preflight := favoriteModpackExportRequest{MinecraftVersion: "1.21.1", Loader: "fabric"}
	result := decodeTEST022Data[favoriteModpackExportPreview](t, recover(base+"/preflight", preflight, 200))
	if result.PreviewID == "" || result.PreviewHash == "" || result.CollectionItemCount != 1 {
		t.Fatalf("actual preflight=%+v", result)
	}
	// This separate typed preview is an explicit synthetic provider-contract
	// fixture. It proves the HTTP job transaction, not a live vendor download or
	// that the earlier no-source preflight produced this file.
	var collectionID, routeID int64
	if err := f.db.QueryRow(f.ctx, `select id from favorite_collections where public_id=$1`, collection.ID).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, f.modID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	fixturePreview := favoriteModpackExportPreview{favoriteModpackExportPreviewSnapshot: favoriteModpackExportPreviewSnapshot{CollectionID: collectionID, CollectionPublicID: collection.ID, CollectionName: "Atomic export collection", MinecraftVersion: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14", CollectionItemCount: 1, ExportedModCount: 1, Items: []favoriteModpackExportItem{{SourceProjectRouteID: &routeID, SourceProjectID: f.modCode, SourceProjectType: "mod", SourceProjectName: "Synthetic selected Mod", ResultType: "exported", ModrinthProjectID: "project", ModrinthVersionID: "version", SelectedVersionName: "1.0", SelectedFileName: "synthetic.jar", MinecraftVersion: "1.21.1", Loader: "fabric", ReleaseType: "release", EnvironmentClient: "required", EnvironmentServer: "required", FileSize: 123, SHA1: strings.Repeat("1", 40), SHA512: strings.Repeat("2", 128), DownloadURL: "https://cdn.modrinth.com/data/project/versions/version/synthetic.jar", DependencyOf: []string{}}}}}
	var err error
	fixturePreview, err = f.origin.Config.Handler.(*Server).persistFavoriteModpackExportPreview(f.ctx, f.userIDs[f.editor], fixturePreview)
	if err != nil {
		t.Fatal(err)
	}
	preflight.PreviewID, preflight.PreviewHash = fixturePreview.PreviewID, fixturePreview.PreviewHash
	task := decodeTEST022Data[struct {
		TaskID string `json:"taskId"`
	}](t, recover(base, preflight, 202))
	var tasks, envelopes, consumed int
	if err = f.db.QueryRow(f.ctx, `select (select count(*) from favorite_modpack_export_tasks),(select count(*) from nats_outbox where aggregate_type='favorite_modpack_export'),(select count(*) from favorite_modpack_export_previews where public_id=$1 and consumed_at is not null)`, fixturePreview.PreviewID).Scan(&tasks, &envelopes, &consumed); err != nil || tasks != 1 || envelopes != 1 || consumed != 1 {
		t.Fatalf("export task/envelope/consumed=%d/%d/%d err=%v", tasks, envelopes, consumed, err)
	}
	before := auditFixFavoriteActivityFacts(t, f)
	f.require(t, f.editor, http.MethodPost, base, preflight, 409)
	auditFixRequireProjectFactsUnchanged(t, before, auditFixFavoriteActivityFacts(t, f))
	for _, source := range []string{favoriteModpackExportSourceCurrent, favoriteModpackExportSourceOriginal} {
		preview := decodeTEST022Data[favoriteModpackExportPreview](t, recover("/api/v1/users/me/modpack-exports/"+task.TaskID+"/rebuild-preflight", favoriteModpackExportRebuildRequest{Source: source}, 200))
		if preview.RebuildSource != source || preview.PreviewID == "" {
			t.Fatalf("rebuild=%+v", preview)
		}
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "favorite.modpack_export")
	before = auditFixFavoriteActivityFacts(t, f)
	f.require(t, f.otherEditor, http.MethodPost, "/api/v1/users/me/modpack-exports/"+task.TaskID+"/rebuild-preflight", favoriteModpackExportRebuildRequest{Source: favoriteModpackExportSourceOriginal}, 404)
	auditFixRequireProjectFactsUnchanged(t, before, auditFixFavoriteActivityFacts(t, f))
}
