package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestCreatorProfileWritesRejectMemberSnapshotsBeforeDatabaseAccess(t *testing.T) {
	server := &Server{}
	for name, handler := range map[string]http.HandlerFunc{
		"create": server.createCreator,
		"update": server.updateCreator,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/api/v1/creators/team00001", strings.NewReader(`{"members":[]}`))
			request.SetPathValue("publicId", "team00001")
			response := httptest.NewRecorder()
			handler(response, request)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "members endpoint") {
				t.Fatalf("profile member payload returned %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestCreatorMemberWriteRejectsContentReviewerBeforeDatabaseAccess(t *testing.T) {
	claims := security.Claims{PermissionRules: []security.PermissionRule{{Code: "content.review", Allow: true, Priority: 100}}}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/creators/team00001/members", strings.NewReader(`{"members":[]}`))
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, claims))
	request.SetPathValue("publicId", "team00001")
	response := httptest.NewRecorder()
	(&Server{}).updateCreatorMembers(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("content reviewer member write returned %d: %s", response.Code, response.Body.String())
	}
}

func TestResolveCreatorCapabilitiesKeepsReviewAndSensitiveWritesSeparate(t *testing.T) {
	claims := func(code string) security.Claims {
		return security.Claims{PermissionRules: []security.PermissionRule{{Code: code, Allow: true, Priority: 100}}}
	}
	if got := resolveCreatorCapabilities(claims("content.review"), "team", "team00001"); got != (creatorCapabilitySet{}) {
		t.Fatalf("content reviewer received creator write capabilities: %+v", got)
	}
	if got := resolveCreatorCapabilities(claims("creator.edit.team00001"), "team", "team00001"); !got.EditProfile || got.ManageMembers || got.CreateRoles {
		t.Fatalf("scoped profile editor capabilities = %+v", got)
	}
	if got := resolveCreatorCapabilities(claims("team.members.manage"), "team", "team00001"); got.EditProfile || !got.ManageMembers || got.CreateRoles {
		t.Fatalf("member manager capabilities = %+v", got)
	}
	if got := resolveCreatorCapabilities(claims("team.members.manage"), "author", "author001"); got.ManageMembers {
		t.Fatalf("author record received team member management: %+v", got)
	}
	if got := resolveCreatorCapabilities(claims("creator.role.write"), "team", "team00001"); got.EditProfile || got.ManageMembers || !got.CreateRoles {
		t.Fatalf("role writer capabilities = %+v", got)
	}
	if got := resolveCreatorCapabilities(claims("admin.*"), "team", "team00001"); !got.EditProfile || !got.ManageMembers || !got.CreateRoles {
		t.Fatalf("administrator capabilities = %+v", got)
	}
}

func TestNormalizeCreatorTeamMembers(t *testing.T) {
	members, err := normalizeCreatorTeamMembers([]creatorMemberPayload{{
		CreatorID: " AUTHOR001 ",
		RoleID:    " ROLE00001 ",
		Title:     " Maintainer ",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].CreatorID != "author001" || members[0].RoleID != "role00001" || members[0].Title != "Maintainer" {
		t.Fatalf("normalized members = %+v", members)
	}

	for name, input := range map[string][]creatorMemberPayload{
		"duplicate": {
			{CreatorID: "author001", RoleID: "role00001"},
			{CreatorID: "AUTHOR001", RoleID: "ROLE00001"},
		},
		"invalid identity": {{CreatorID: "not valid", RoleID: "role00001"}},
		"oversized title":  {{CreatorID: "author001", RoleID: "role00001", Title: strings.Repeat("界", 54)}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeCreatorTeamMembers(input); err == nil {
				t.Fatal("invalid team members were accepted")
			}
		})
	}

	overLimit := make([]creatorMemberPayload, maximumCreatorTeamMembers+1)
	if _, err := normalizeCreatorTeamMembers(overLimit); err == nil {
		t.Fatal("member limit was not enforced before relation processing")
	}
}
