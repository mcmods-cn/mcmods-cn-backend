package database

import (
	"strings"
	"testing"
)

func TestReportsSchemaAllowsModpackTargets(t *testing.T) {
	t.Parallel()
	if schemaGeneration != 167 {
		t.Fatalf("modpack report target requires schema generation 167, got %d", schemaGeneration)
	}
	schema := strings.Join(governanceAutomationSchemaStatements(), "\n")
	if !strings.Contains(schema, "'mod','modpack','plugin'") {
		t.Fatal("reports target_type check does not include modpack")
	}
}
