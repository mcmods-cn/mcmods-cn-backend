package database

import (
	"strings"
	"testing"
)

func TestAITaskRecoveryHasMatchingIndexes(t *testing.T) {
	definition := strings.ToLower(strings.Join(baselineSchemaStatements(), "\n") + "\n" + strings.Join(infrastructureSchemaStatements(), "\n"))
	for _, required := range []string{
		"idx_ai_tasks_recovery",
		"on ai_tasks(status,updated_at,id)",
		"where status in ('queued','retrying','running')",
		"idx_nats_outbox_aggregate",
		"on nats_outbox(aggregate_type,aggregate_id,id)",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("AI recovery schema is missing %q", required)
		}
	}
}
