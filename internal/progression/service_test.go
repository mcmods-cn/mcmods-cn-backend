package progression

import (
	"testing"
	"time"

	"mcmods-cn-backend/internal/activity"
)

func TestMatchesTaskSupportsCatalogResources(t *testing.T) {
	event := activity.Event{
		ActionID:       activity.ActionEdit,
		ObjectTypeID:   activity.ObjectResource,
		ObjectPublicID: "abc123xyz",
	}
	condition := taskCondition{
		Action:         "edit",
		ObjectType:     "resource",
		ObjectPublicID: "abc123xyz",
		Metric:         "count",
		Target:         1,
	}
	if !matchesTask(condition, event) {
		t.Fatal("catalog resource activity should match the corresponding task")
	}
	condition.ObjectPublicID = "different"
	if matchesTask(condition, event) {
		t.Fatal("task constrained to another resource must not match")
	}
}

func TestMatchesTaskRejectsUnknownConditionCodes(t *testing.T) {
	event := activity.Event{ActionID: activity.ActionEdit, ObjectTypeID: activity.ObjectResource}
	for _, condition := range []taskCondition{
		{Action: "unknown", ObjectType: "resource", Metric: "count"},
		{Action: "edit", ObjectType: "unknown", Metric: "count"},
		{Action: "edit", ObjectType: "resource", Metric: "unknown"},
	} {
		if matchesTask(condition, event) {
			t.Fatalf("invalid task condition matched an activity: %#v", condition)
		}
	}
}

func TestPeriodKeyUsesUserTimezone(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	timestamp := time.Date(2026, time.August, 2, 17, 30, 0, 0, time.UTC)
	if got := PeriodKey("daily", timestamp, location); got != "2026-08-03" {
		t.Fatalf("daily period key = %q, want 2026-08-03", got)
	}
	if got := PeriodKey("monthly", timestamp, location); got != "2026-08" {
		t.Fatalf("monthly period key = %q, want 2026-08", got)
	}
}
