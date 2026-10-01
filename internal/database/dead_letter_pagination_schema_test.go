package database

import (
	"strings"
	"testing"
)

func TestDeadLetterPagesHaveGeneration125AggregateAndStatusIndexes(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(infrastructureSchemaStatements(), "\n"))
	for _, required := range []string{
		"aggregate_type text", "aggregate_id text",
		"idx_dead_letter_events_pending", "failed_at desc,id desc",
		"where replayed_at is null", "idx_dead_letter_events_replayed",
		"where replayed_at is not null", "idx_dead_letter_events_aggregate",
		"aggregate_type,aggregate_id,failed_at desc,id desc",
	} {
		if !strings.Contains(schema, required) {
			t.Errorf("dead-letter pagination schema is missing %q", required)
		}
	}
}
