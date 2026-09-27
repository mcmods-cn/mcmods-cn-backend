package database

import (
	"strings"
	"testing"
)

func TestAuthorizationBindingsHaveExplicitIndependentSources(t *testing.T) {
	definition := strings.ToLower(strings.Join(baselineSchemaStatements(), "\n"))
	for _, required := range []string{
		"source text not null default 'manual'",
		"source_key text not null default ''",
		"primary key (user_id, role_id, source, source_key)",
		"primary key (user_id, permission_id, source, source_key)",
		"source in ('manual','account_status','governance_ban','level_track')",
		"source in ('manual','user_preference','system_seed')",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("authorization schema is missing %q", required)
		}
	}
	userPermissionStart := strings.Index(definition, "create table if not exists user_permissions")
	if userPermissionStart < 0 {
		t.Fatal("could not isolate the user_permissions schema")
	}
	userPermissionEnd := strings.Index(definition[userPermissionStart:], "create index if not exists idx_role_permissions")
	if userPermissionEnd < 0 {
		t.Fatal("could not isolate the user_permissions schema")
	}
	userPermissionDefinition := definition[userPermissionStart : userPermissionStart+userPermissionEnd]
	if strings.Contains(userPermissionDefinition, "context text") {
		t.Fatal("direct permission context still looks like an authorization scope without enforcement semantics")
	}
}
