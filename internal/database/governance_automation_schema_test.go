package database

import (
	"strings"
	"testing"
)

func TestGovernanceAutomationSchemaUsesCanonicalSeedProjectTypes(t *testing.T) {
	statements := make([]string, 0)
	for _, statement := range governanceAutomationSchemaStatements() {
		if strings.Contains(statement, "seed_crawler") {
			statements = append(statements, statement)
		}
	}
	definition := strings.Join(statements, "\n")
	for _, required := range []string{"'mod'", "'plugin'", "'shader_pack'", "'resource_pack'"} {
		if !strings.Contains(definition, required) {
			t.Fatalf("seed crawler schema is missing canonical type %s", required)
		}
	}
	if strings.Contains(definition, "'shader','resource_pack'") {
		t.Fatal("seed crawler schema still uses the obsolete shader value")
	}
}

func TestGovernanceAutomationSchemaRecordsAutomationActorAndMaintenanceOwnership(t *testing.T) {
	definition := strings.Join(governanceAutomationSchemaStatements(), "\n")
	for _, required := range []string{
		"actor_id bigint references users(id) on delete set null",
		"create table project_automation_activity",
		"last_project_change_at timestamptz not null",
		"has_external_activity boolean not null default false",
		"automated_status text",
		"status_before_automation text not null default ''",
		"manual_status_override boolean not null default false",
		"changed_by bigint references users(id) on delete set null",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("automation schema is missing %q", required)
		}
	}
}
