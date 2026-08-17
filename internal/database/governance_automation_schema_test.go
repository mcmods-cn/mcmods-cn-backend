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
