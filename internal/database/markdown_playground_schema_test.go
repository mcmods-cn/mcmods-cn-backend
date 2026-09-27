package database

import (
	"strings"
	"testing"
)

func TestMarkdownPlaygroundRevisionContractIsInstalledInCurrentSchema(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("unexpected schema generation %d", schemaGeneration)
	}
	source := strings.Join(baselineSchemaStatements(), "\n")
	for _, fragment := range []string{
		"revision bigint not null default 1",
		"save_session_id text not null default ''",
		"client_sequence bigint not null default 0",
		"check(revision > 0)",
		"check(client_sequence >= 0)",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("markdown draft schema is missing %q", fragment)
		}
	}
}
