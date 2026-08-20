package database

import (
	"strings"
	"testing"
)

func TestPermissionChangesDoNotInvalidateAuthenticationSessions(t *testing.T) {
	schema := strings.ToLower(strings.Join(append(baselineSchemaStatements(), infrastructureSchemaStatements()...), "\n"))
	for _, required := range []string{
		"permission_version bigint not null default 1",
		"bump_direct_user_permission_version",
		"trg_user_role_bindings_permission_version",
		"trg_user_permissions_permission_version",
		"set permission_version=permission_version+1",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("permission version schema is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"trg_user_role_bindings_auth_version",
		"trg_user_permissions_auth_version",
		"bump_role_users_auth_version",
		"bump_all_user_auth_versions",
	} {
		if strings.Contains(schema, forbidden) {
			t.Fatalf("permission change still invalidates sessions through %q", forbidden)
		}
	}
}

func TestCatalogProjectsUseSubmitterFactsInsteadOfOwnerFields(t *testing.T) {
	schema := strings.ToLower(strings.Join(append(append(baselineSchemaStatements(), modpackSchemaStatements()...), simpleProjectSchemaStatements()...), "\n"))
	for _, required := range []string{
		"submitted_by bigint references users(id)",
		"idx_mods_submitted_by_updated_at",
		"idx_modpacks_submitted_by",
		"idx_simple_projects_submitted_by",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("catalog submission schema is missing %q", required)
		}
	}
}

func TestServerRecordsAProjectSubmitterWithoutCreatingOwnership(t *testing.T) {
	schema := strings.ToLower(strings.Join(serverSchemaStatements(), "\n"))
	if !strings.Contains(schema, "submitted_by bigint not null references users(id)") {
		t.Fatal("server schema does not record its submitter")
	}
	if strings.Contains(schema, "created_by bigint") {
		t.Fatal("server schema still presents its submitter as an owner")
	}
}
