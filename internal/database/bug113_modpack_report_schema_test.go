package database

import (
	"strings"
	"testing"
)

func TestReportsSchemaAllowsModpackTargets(t *testing.T) {
	t.Parallel()
	if schemaGeneration != 168 {
		t.Fatalf("modpack report target requires schema generation 168, got %d", schemaGeneration)
	}
	schema := strings.Join(governanceAutomationSchemaStatements(), "\n")
	if !strings.Contains(schema, "'mod','modpack','plugin'") {
		t.Fatal("reports target_type check does not include modpack")
	}
}
