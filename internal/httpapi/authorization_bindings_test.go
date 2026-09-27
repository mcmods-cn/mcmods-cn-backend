package httpapi

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLegacyUserRoleReplacementRouteIsRemoved(t *testing.T) {
	routes, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	handlers, err := os.ReadFile("admin_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(routes), `PUT /api/v1/admin/users/{id}/roles`) ||
		strings.Contains(string(handlers), "func (s *Server) updateUserRoles") {
		t.Fatal("destructive legacy user-role replacement API is still registered")
	}
}

func TestRoleTrackMutationLocksUserBeforeReadingManualBindings(t *testing.T) {
	raw, err := os.ReadFile("role_track_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func (s *Server) applyUserRoleTrack")
	end := strings.Index(source, "func shiftRoleTrackRoles")
	if start < 0 || end <= start {
		t.Fatal("could not isolate applyUserRoleTrack")
	}
	implementation := source[start:end]
	lockIndex := strings.Index(implementation, "select id from users where id=$1 for update")
	readIndex := strings.Index(implementation, "select role.code,binding.expires_at from user_role_bindings")
	if lockIndex < 0 || readIndex < 0 || lockIndex > readIndex {
		t.Fatal("role-track mutation does not lock the user before reading its manual authorization snapshot")
	}
	for _, required := range []string{"binding.source=$2", "rows.Err()", "tx.Query("} {
		if !strings.Contains(implementation, required) {
			t.Fatalf("role-track mutation is missing concurrency guard %q", required)
		}
	}
	if strings.Contains(implementation, "s.db.Query(") {
		t.Fatal("role-track mutation still reads its authorization snapshot outside the transaction")
	}
}

func TestNormalizeManualAuthorizationEntriesRejectsNonManualAndDeniedGroups(t *testing.T) {
	for _, entries := range [][]userPermissionEntry{
		{{Code: "group.admin", Allow: false}},
		{{Code: "group.admin", Allow: true, Source: authorizationSourceLevelTrack}},
		{{Code: "group.admin", Allow: true, SourceKey: "forged"}},
	} {
		if _, _, err := normalizeManualAuthorizationEntries(entries); err == nil {
			t.Fatalf("unsafe authorization entries were accepted: %#v", entries)
		}
	}
}

func TestNormalizeManualAuthorizationEntriesPreservesRoleExpiry(t *testing.T) {
	expiresAt := "2027-01-02T03:04:05Z"
	roles, permissions, err := normalizeManualAuthorizationEntries([]userPermissionEntry{
		{Code: "group.temporary_admin", Allow: true, ExpiresAt: expiresAt, Source: authorizationSourceManual},
	})
	if err != nil || len(roles) != 1 || len(permissions) != 0 || roles[0].ExpiresAt == nil {
		t.Fatalf("normalized role grant = %#v, %#v, %v", roles, permissions, err)
	}
	if roles[0].Code != "temporary_admin" || roles[0].ExpiresAt.Format(time.RFC3339) != expiresAt {
		t.Fatalf("role expiry was not preserved: %#v", roles[0])
	}
	if strings.TrimSpace(roles[0].Code) == "" {
		t.Fatal("role code was lost")
	}
}
