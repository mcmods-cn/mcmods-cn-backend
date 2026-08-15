package userstats

import (
	"strings"
	"testing"
)

func TestNormalizeOptions(t *testing.T) {
	value, err := NormalizeOptions(Options{})
	if err != nil || value.BatchSize != 200 {
		t.Fatalf("unexpected defaults: %#v %v", value, err)
	}
	if _, err = NormalizeOptions(Options{BatchSize: 1001}); err == nil {
		t.Fatal("expected oversized batch to be rejected")
	}
}

func TestReconcileSQLIsIdempotentAndKeepsExactActivityKinds(t *testing.T) {
	t.Parallel()
	for _, fragment := range []string{
		"greatest(user_statistics_daily.action_count,excluded.action_count)",
		"greatest(user_statistics_totals.action_count,excluded.action_count)",
		"event.action_id=1",
		"event.action_id=2 and event.object_type_id=8",
	} {
		if !strings.Contains(activityDailyBackfillSQL+activityTotalReconcileSQL, fragment) {
			t.Fatalf("statistics reconciliation SQL is missing %q", fragment)
		}
	}
}
