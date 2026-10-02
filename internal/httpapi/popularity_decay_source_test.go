package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestPopularityDecaySchedulerClaimsOnlyIndexedDueRows(t *testing.T) {
	source, err := os.ReadFile("popularity_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{
		"enqueueTimeDependentStats(ctx, db, true)",
		"enqueueTimeDependentStats(ctx, db, false)",
		"stats.next_decay_at<=now()",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("decay scheduler is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"exists(select 1 from content_popularity_events_daily",
		"exists(select 1 from content_heat_promotions",
		"content_target_created_at(route.id)>now()",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("decay scheduler retained fleet-wide derived scan %q", forbidden)
		}
	}
}
