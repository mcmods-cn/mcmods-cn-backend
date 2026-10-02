package database

import (
	"strings"
	"testing"
)

func TestReportsSchemaNamesTargetActorsWithoutCallingSubmittersAuthors(t *testing.T) {
	t.Parallel()
	if schemaGeneration != 168 {
		t.Fatalf("reports target-actor contract requires schema generation 168, got %d", schemaGeneration)
	}
	schema := strings.Join(governanceAutomationSchemaStatements(), "\n")
	for _, required := range []string{"target_actor_id bigint", "target_actor_role text not null", "'submitter'", "'author'", "'owner'", "'subject'"} {
		if !strings.Contains(schema, required) {
			t.Errorf("reports schema is missing %q", required)
		}
	}
	if strings.Contains(schema, "target_author_id") {
		t.Fatal("reports schema still conflates the target submitter with an author")
	}
}
