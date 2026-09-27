package database

import (
	"os"
	"strings"
	"testing"
)

func TestBlueprintJobAdmissionHasGeneration125ActiveCreatorIndex(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("schema generation=%d want 167", schemaGeneration)
	}
	payload, err := os.ReadFile("migrations.go")
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ToLower(string(payload))
	for _, required := range []string{
		"idx_blueprint_jobs_creator_active",
		"blueprint_jobs(created_by,id)",
		"where status in ('queued','processing') and created_by is not null",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("blueprint job schema is missing %q", required)
		}
	}
}
