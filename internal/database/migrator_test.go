package database

import (
	"strings"
	"testing"
)

func TestServerSchemaFunctionsAreReplaceable(t *testing.T) {
	for index, statement := range serverSchemaStatements() {
		normalized := strings.ToLower(strings.TrimSpace(statement))
		if strings.HasPrefix(normalized, "create function ") {
			t.Errorf("server schema statement %d creates a non-replaceable function", index)
		}
	}
}

func TestDevelopmentResetRemovesStandalonePublicFunctions(t *testing.T) {
	statement := resetDevelopmentSchemaStatement()
	for _, fragment := range []string{
		"from pg_proc procedure_row",
		"pg_get_function_identity_arguments(procedure_row.oid)",
		"dependency_row.deptype='e'",
		"drop %s if exists public.%I(%s) cascade",
	} {
		if !strings.Contains(statement, fragment) {
			t.Errorf("development reset is missing function cleanup fragment %q", fragment)
		}
	}
}
