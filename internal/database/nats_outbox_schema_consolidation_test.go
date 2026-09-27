package database

import (
	"strings"
	"testing"
)

func TestNATSOutboxSchemaIsDefinedOnceInFinalShape(t *testing.T) {
	baseline := strings.ToLower(strings.Join(baselineSchemaStatements(), "\n"))
	infrastructure := strings.ToLower(strings.Join(infrastructureSchemaStatements(), "\n"))
	for _, required := range []string{
		"event_type text not null default ''",
		"schema_version integer not null default 1 check(schema_version > 0)",
		"occurred_at timestamptz not null default now()",
		"trace_id text not null default ''",
		"status text not null default 'pending'",
		"available_at timestamptz not null default now()",
		"locked_at timestamptz",
		"locked_by text not null default ''",
		"max_attempts integer not null default 12 check(max_attempts > 0)",
		"updated_at timestamptz not null default now()",
		"constraint nats_outbox_status_check check(status in ('pending','publishing','published','failed','dead'))",
		"idx_nats_outbox_pending on nats_outbox(available_at,id)",
		"idx_nats_outbox_status_created on nats_outbox(status,created_at)",
		"idx_nats_outbox_aggregate on nats_outbox(aggregate_type,aggregate_id,id)",
	} {
		if !strings.Contains(baseline, required) {
			t.Errorf("authoritative nats_outbox definition is missing %q", required)
		}
	}
	for _, transition := range []string{
		"alter table nats_outbox",
		"update nats_outbox",
		"drop index if exists idx_nats_outbox_pending",
	} {
		if strings.Contains(infrastructure, transition) {
			t.Errorf("same-generation nats_outbox transition remains: %q", transition)
		}
	}
	if strings.Contains(baseline, "on nats_outbox(created_at) where published_at is null") {
		t.Fatal("authoritative schema still creates the obsolete created_at-only pending index")
	}
}
