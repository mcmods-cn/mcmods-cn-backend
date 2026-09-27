package database

import (
	"os"
	"strings"
	"testing"
)

func TestSeedCrawlerCandidateProvenanceUsesGeneration148FirstLastAndSubmissionState(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("schema generation=%d want 167", schemaGeneration)
	}
	raw, err := os.ReadFile("governance_automation_schema.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, required := range []string{
		"first_seen_run_id bigint not null references seed_crawler_runs(id)",
		"last_seen_run_id bigint not null references seed_crawler_runs(id)",
		"idx_seed_crawler_candidates_first_seen_run on seed_crawler_candidates(first_seen_run_id)",
		"idx_seed_crawler_candidates_last_seen_run on seed_crawler_candidates(last_seen_run_id)",
		"check(status in ('candidate','submitting','failed','existing','draft','submitted'))",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("seed crawler candidate schema is missing %q", required)
		}
	}
	if strings.Contains(source, "\n\t\t\trun_id bigint references seed_crawler_runs") {
		t.Fatal("ambiguous candidate run_id remains in the authoritative schema")
	}
}
