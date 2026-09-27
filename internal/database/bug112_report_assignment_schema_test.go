package database

import (
	"strings"
	"testing"
)

func TestReportAssignmentSchemaMakesInReviewOwnershipAuditable(t *testing.T) {
	t.Parallel()
	if schemaGeneration != 167 {
		t.Fatalf("report assignment authority requires schema generation 167, got %d", schemaGeneration)
	}
	schema := strings.Join(governanceAutomationSchemaStatements(), "\n")
	for _, fragment := range []string{
		"status<>'in_review' or (claimed_by is not null and claimed_at is not null)",
		"create table report_assignment_events",
		"check(action in ('claim','takeover'))",
		"check(action<>'takeover' or btrim(reason)<>'')",
		"idx_report_assignment_events_report",
	} {
		if !strings.Contains(schema, fragment) {
			t.Fatalf("report assignment schema missing %q", fragment)
		}
	}
}
