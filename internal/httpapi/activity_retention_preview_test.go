package httpapi

import (
	"strings"
	"testing"
	"time"
)

func TestActivityCleanupPreviewUsesOneBoundedMaterializedCandidateSet(t *testing.T) {
	query := activityCleanupPreviewSummarySQL("event.occurred_at<$1 and event.action_id=any($2::smallint[])", 3)
	if strings.Count(query, "from user_activity_events event") != 1 {
		t.Fatalf("preview query must scan the activity table once:\n%s", query)
	}
	for _, required := range []string{
		"with candidates as materialized",
		"limit $3",
		"from candidates candidate",
		"jsonb_object_agg",
		"sample_rows",
	} {
		if !strings.Contains(query, required) {
			t.Fatalf("preview query is missing %q", required)
		}
	}
	if strings.Contains(query, "activityCleanupGroupedCounts") {
		t.Fatal("preview query retained the per-dimension scan helper")
	}
}

func TestActivityCleanupPreviewRejectsOnlySyntacticallyUnboundedFilters(t *testing.T) {
	now := time.Now().UTC()
	base := normalizedActivityCleanupFilter{ActionIDs: []int16{3}, SnapshotBefore: now}
	if !activityCleanupPreviewFilterIsUnbounded(base) {
		t.Fatal("action-only all-history preview must be rejected before querying")
	}
	for name, filter := range map[string]normalizedActivityCleanupFilter{
		"user":        {ActionIDs: base.ActionIDs, UserIDs: []int64{1}, SnapshotBefore: now},
		"object":      {ActionIDs: base.ActionIDs, ObjectRouteIDs: []int64{1}, SnapshotBefore: now},
		"object type": {ActionIDs: base.ActionIDs, ObjectTypeIDs: []int16{2}, SnapshotBefore: now},
		"from":        {ActionIDs: base.ActionIDs, From: &now, SnapshotBefore: now},
		"to":          {ActionIDs: base.ActionIDs, To: &now, SnapshotBefore: now},
	} {
		if activityCleanupPreviewFilterIsUnbounded(filter) {
			t.Fatalf("%s filter was rejected as syntactically unbounded", name)
		}
	}
}
