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

func TestReconcileSQLExactlyReplacesAllStatisticsFromRawAndRetainedFacts(t *testing.T) {
	t.Parallel()
	for _, fragment := range []string{
		"select 1 from users where id=$1 for update",
		"from user_statistics_retained_actions retained",
		"action_count=excluded.action_count",
		"action_counts=excluded.action_counts",
		"not exists(select 1 from rolled",
		"jsonb_each_text(daily.action_counts)",
		"event.action_id=1",
		"event.action_id=2 and event.object_type_id=8",
	} {
		if !strings.Contains(reconcileUserLockSQL+activityDailyBackfillSQL+activityTotalReconcileSQL, fragment) {
			t.Fatalf("statistics reconciliation SQL is missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"greatest(user_statistics_daily.",
		"greatest(user_statistics_totals.",
		"case when excluded.action_count>",
	} {
		if strings.Contains(activityDailyBackfillSQL+activityTotalReconcileSQL, forbidden) {
			t.Fatalf("statistics reconciliation SQL retains monotonic drift guard %q", forbidden)
		}
	}
}
