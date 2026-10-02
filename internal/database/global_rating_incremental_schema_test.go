package database

import (
	"strings"
	"testing"
)

func TestGlobalRatingStatsUseGeneration125WriteMaintainedFacts(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168", schemaGeneration)
	}
	statements := ratingSchemaStatements()
	schema := strings.ToLower(strings.Join(statements, "\n"))
	for _, required := range []string{
		"adjust_content_rating_global_stats",
		"rebuild_content_rating_global_stats",
		"perform adjust_content_rating_global_stats",
	} {
		if !strings.Contains(schema, required) {
			t.Errorf("incremental global rating schema is missing %q", required)
		}
	}
	if strings.Contains(schema, "create or replace function refresh_content_rating_global_stats") {
		t.Error("per-task global rating refresh function remains installed")
	}

	var rebuild string
	for _, statement := range statements {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(statement)), "create or replace function rebuild_content_rating_global_stats(") {
			rebuild = strings.ToLower(statement)
			break
		}
	}
	if rebuild == "" {
		t.Fatal("set-based global rating rebuild function is missing")
	}
	for _, required := range []string{
		"route_developers as",
		"join effective_project_access access",
		"group by route.id,access.user_id",
		"not exists(select 1 from route_developers developer",
	} {
		if !strings.Contains(rebuild, required) {
			t.Errorf("global rating rebuild is missing %q", required)
		}
	}
	for _, forbidden := range []string{"content_target_owner_id(", "min(access.user_id)", "route_owners"} {
		if strings.Contains(rebuild, forbidden) {
			t.Errorf("global rating rebuild still contains lossy developer mapping %q", forbidden)
		}
	}
}
