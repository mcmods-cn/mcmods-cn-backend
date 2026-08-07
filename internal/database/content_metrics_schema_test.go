package database

import (
	"strings"
	"testing"
)

func TestContentMetricsUseCoalescedNumericRouteProjection(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(strings.Join(contentMetricsSchemaStatements(), "\n"))
	for _, required := range []string{
		"create table content_route_metrics",
		"object_route_id bigint primary key references public_routes(id)",
		"create table content_stats_refresh_queue",
		"enqueue_content_stats_refresh",
		"refresh_content_route_metrics",
		"trg_change_requests_metrics",
		"trg_catalog_import_revisions_metrics",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("content metrics schema does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"object_public_id", "target_public_id", "metadata jsonb"} {
		if strings.Contains(definition, forbidden) {
			t.Fatalf("content metrics schema contains character relation or high-volume metadata %q", forbidden)
		}
	}
}
