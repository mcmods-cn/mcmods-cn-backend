package database

import (
	"strings"
	"testing"
)

func TestPopularityRefreshUsesGeneration125IncrementalFacts(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("schema generation=%d want 167", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(ratingSchemaStatements(), "\n"))
	for _, required := range []string{
		"create table content_popularity_lifetime_facts",
		"create table content_popularity_view_totals",
		"create table content_popularity_rating_dimension_facts",
		"adjust_content_popularity_lifetime_facts",
		"trg_content_view_daily_popularity_facts",
		"trg_content_unique_views_popularity_facts",
		"trg_content_project_pages_popularity_facts",
		"trg_content_download_counters_popularity_facts",
		"trg_content_rating_scores_popularity_facts",
		"rebuild_content_popularity_lifetime_facts",
		"next_decay_at timestamptz",
		"idx_content_popularity_stats_decay_due",
	} {
		if !strings.Contains(schema, required) {
			t.Errorf("incremental popularity schema is missing %q", required)
		}
	}

	refresh := strings.ToLower(contentPopularityRefreshFunctionStatement())
	for _, required := range []string{
		"from content_popularity_lifetime_facts",
		"from content_popularity_view_totals",
		"from content_popularity_rating_dimension_facts",
		"event_date>=current_date-90",
		"next_decay_at",
	} {
		if !strings.Contains(refresh, required) {
			t.Errorf("hot popularity refresh is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"from content_view_daily",
		"from content_unique_views",
		"from content_project_pages",
		"from content_download_counters",
		"from favorite_collection_items",
		"from comments",
		"from content_ratings",
		"from content_rating_scores",
	} {
		if strings.Contains(refresh, forbidden) {
			t.Errorf("hot popularity refresh retained lifetime scan %q", forbidden)
		}
	}
}
