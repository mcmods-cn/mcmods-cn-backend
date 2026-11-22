package database

import (
	"strings"
	"testing"
)

func TestReportsSchemaAllowsModpackTargets(t *testing.T) {
	t.Parallel()
	schema := strings.Join(governanceAutomationSchemaStatements(), "\n")
	if !strings.Contains(schema, "'mod','modpack','plugin'") {
		t.Fatal("reports target_type check does not include modpack")
	}
}
