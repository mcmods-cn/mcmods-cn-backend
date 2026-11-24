package httpapi

import (
	"net/http"
	"testing"
)

func TestContentMetricsAttributeImportEditsOnPublicationIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", "Metrics publication")
	section := f.section(t, version.PublicID, f.builtin(t, "advancement"))
	resource := f.resource(t, version.PublicID, section.PublicID, "test013:metrics", "minecraft.advancement", "advancement", map[string]any{})
	f.review(t, resource, f.editor, "approved")

	var routeID, resourceID int64
	if err := f.db.QueryRow(f.ctx, `select id,internal_id from public_routes
		where entity_type='resource' and public_id=$1`, resource.PublicID).Scan(&routeID, &resourceID); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := f.db.Exec(f.ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	metrics := func() contentMetricsResponse {
		t.Helper()
		exec("select refresh_content_route_metrics($1)", routeID)
		return decodeTEST022Data[contentMetricsResponse](t, f.require(t, "", http.MethodGet,
			"/api/v1/content-metrics/"+resource.PublicID, nil, http.StatusOK))
	}
	base := metrics()
	editorCount := func(result contentMetricsResponse) int64 {
		var count int64
		for _, editor := range result.RecentEditors {
			count += editor.Count
		}
		return count
	}
	assert := func(stage string, additional int64) {
		t.Helper()
		got := metrics()
		if got.EditCount != base.EditCount+additional || editorCount(got) != editorCount(base)+additional {
			t.Fatalf("%s: stored total %d, recent editors %d; baseline %d/%d, additional %d", stage,
				got.EditCount, editorCount(got), base.EditCount, editorCount(base), additional)
		}
	}
	revision := oct02CInsertPublishedExportRevision(t, f, version.PublicID)
	exec("update catalog_import_revisions set is_active=false,activated_at=null where id=$1", revision)
	exec(`insert into resource_import_snapshots(id,resource_id,revision_id,registry) values($1,$2,$3,'advancements')`,
		newExportID(), resourceID, revision)
	assert("import ready for review", 0)
	exec("delete from content_stats_refresh_queue where object_route_id=$1", routeID)
	exec("update catalog_import_revisions set is_active=true,activated_at=now() where id=$1", revision)
	var queued bool
	if err := f.db.QueryRow(f.ctx, `select exists(select 1 from content_stats_refresh_queue
		where object_route_id=$1 and refresh_metrics)`, routeID).Scan(&queued); err != nil || !queued {
		t.Fatalf("publish queued the refresh: %t, %v", queued, err)
	}
	assert("published", 1)
	exec("update catalog_import_revisions set status='superseded',is_active=false where id=$1", revision)
	assert("published contribution retained in history", 1)
	exec("update mod_content_versions set status='archived' where public_id=$1", version.PublicID)
	assert("source version archived", 0)
}
