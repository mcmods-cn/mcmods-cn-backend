package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestCreatorProfileAndTeamMemberCapabilitiesAreSeparated(t *testing.T) {
	handler, err := os.ReadFile("creator_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	server, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	members, err := os.ReadFile("creator_member_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	combined := string(handler) + string(server) + string(members)
	for _, required := range []string{
		`"canEditProfile"`,
		`"canManageMembers"`,
		`"canCreateRoles"`,
		`PUT /api/v1/creators/{publicId}/members`,
		`creator.team_members.replace`,
		`permission_audit_logs`,
		`replaceCreatorTeamMembersTx`,
	} {
		if !strings.Contains(combined, required) {
			t.Errorf("creator capability contract is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`"canEdit": canEdit`,
		`team membership changes require team.members.manage`,
	} {
		if strings.Contains(combined, forbidden) {
			t.Errorf("ambiguous creator edit path remains: %q", forbidden)
		}
	}
}
