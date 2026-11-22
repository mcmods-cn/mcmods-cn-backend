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
