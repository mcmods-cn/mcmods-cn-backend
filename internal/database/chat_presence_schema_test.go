package database

import (
	"strings"
	"testing"
)

func TestChatPresenceHasNoUnusedPostgreSQLTable(t *testing.T) {
	definition := strings.ToLower(strings.Join(schemaInstallationStatements(), "\n"))
	if strings.Contains(definition, "user_chat_presence") {
		t.Fatal("unused PostgreSQL chat presence authority remains in the schema")
	}
}
