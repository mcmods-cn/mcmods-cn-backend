package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestAdminAuthorizationReadsPropagateEveryDatabaseError(t *testing.T) {
	raw, err := os.ReadFile("admin_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func (s *Server) rolePermissionEntries")
	end := strings.Index(source, "func rolePermissionCodes")
	if start < 0 || end <= start {
		t.Fatal("role permission reader inventory changed")
	}
	reader := source[start:end]
	for _, required := range []string{
		"([]domain.RolePermissionEntry, error)",
		"return nil, err",
		"return entriesByRole, rows.Err()",
	} {
		if !strings.Contains(reader, required) {
			t.Fatalf("role permission reader does not propagate %q", required)
		}
	}
	if strings.Contains(reader, "return []domain.RolePermissionEntry{}") ||
		strings.Contains(reader, "if err := rows.Scan(&entry.Code, &entry.Allow, &expiresAt); err == nil") {
		t.Fatal("role permission reader still converts query/scan failures into empty or partial data")
	}

	rolesStart := strings.Index(source, "func (s *Server) roles")
	rolesEnd := strings.Index(source, "func (s *Server) replaceRolePermissions")
	if rolesStart < 0 || rolesEnd <= rolesStart {
		t.Fatal("role response reader inventory changed")
	}
	roleReaders := source[rolesStart:rolesEnd]
	if strings.Count(roleReaders, "rolePermissionEntries(ctx, role.Code)") != 1 ||
		strings.Count(roleReaders, "rolePermissionEntriesForRoles(ctx, roleCodes)") != 1 ||
		strings.Count(roleReaders, "if err != nil") < 4 {
		t.Fatal("role list/detail does not propagate nested permission read errors")
	}

	usersStart := strings.Index(source, "func (s *Server) adminUsers")
	usersEnd := strings.Index(source, "func (s *Server) createAdminUser")
	if usersStart < 0 || usersEnd <= usersStart {
		t.Fatal("admin user reader inventory changed")
	}
	userReader := source[usersStart:usersEnd]
	if !strings.Contains(userReader, "rows.Err()") {
		t.Fatal("admin user list does not check the final row iterator error")
	}
	if !strings.Contains(userReader, "resolveUsersRootPermissions(r.Context(), userIDs)") ||
		strings.Contains(userReader, "resolveUserRootPermissions(r.Context(), user.ID)") {
		t.Fatal("admin user list does not use the existing batch permission resolver")
	}

	createStart := strings.Index(source, "func (s *Server) createRole")
	createEnd := strings.Index(source, "func (s *Server) updateRole")
	if createStart < 0 || createEnd <= createStart || !strings.Contains(source[createStart:createEnd], "isUniqueViolation(err)") {
		t.Fatal("role creation still classifies every insert failure as a duplicate code")
	}
}
