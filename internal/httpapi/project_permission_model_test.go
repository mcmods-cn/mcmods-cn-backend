package httpapi

import (
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestCatalogSubmitterIsNotAProjectEditor(t *testing.T) {
	const userID int64 = 42
	claims := security.Claims{Subject: userID}

	if canEditMod(claims, modIdentityRecord{UniqueID: "a2bc3de89", SubmittedByID: int64Pointer(userID)}) {
		t.Fatal("mod submitter was treated as a project editor")
	}
	if canEditModpack(claims, modpackResponse{PublicID: "b2cd3ef89", SubmittedByInternal: int64Pointer(userID)}) {
		t.Fatal("modpack submitter was treated as a project editor")
	}
	if canEditSimpleProject(claims, simpleProjectResponse{PublicID: "c2de3fg89", SubmittedByInternal: int64Pointer(userID)}) {
		t.Fatal("simple project submitter was treated as a project editor")
	}
}

func TestApprovedScopedRoleAllowsCatalogEditingWithoutSessionReplacement(t *testing.T) {
	const userID int64 = 42
	claims := security.Claims{
		Subject:         userID,
		PermissionRules: []security.PermissionRule{{Code: "project.edit.a2bc3de89", Allow: true}},
	}
	if !canEditMod(claims, modIdentityRecord{UniqueID: "a2bc3de89"}) {
		t.Fatal("approved scoped project permission did not allow editing")
	}
}

func TestAdministratorsCanDefineEmergencyProjectAccess(t *testing.T) {
	for _, req := range []roleRequest{
		{Code: "project_developer.a2bc3de89", Name: "Emergency project developer", Weight: 100},
		{Code: "project_editor.a2bc3de89", Name: "Emergency project editor", Weight: 50},
	} {
		if err := validateRoleRequest(req, true); err != nil {
			t.Fatalf("manual administrator project role %q was rejected: %v", req.Code, err)
		}
	}
	for _, permission := range []string{
		"project.edit.a2bc3de89",
		"project.review.a2bc3de89",
		"project.comment.moderate.a2bc3de89",
		"project.comment.pin.a2bc3de89",
		"project.comment.role.developer.a2bc3de89",
		"project.comment.role.editor.a2bc3de89",
	} {
		if !permissionCodePattern.MatchString(permission) {
			t.Fatalf("manual administrator permission %q should remain configurable", permission)
		}
	}
}

func TestProjectRelationshipReviewPermissionsStayIndependent(t *testing.T) {
	authors, teams := projectRelationshipPermissions(func(permission string) bool {
		return permission == "project.team_relation.manage"
	})
	if authors || !teams {
		t.Fatalf("team-only reviewer resolved authors=%t teams=%t", authors, teams)
	}
	authors, teams = projectRelationshipPermissions(func(permission string) bool {
		return permission == "project.authorship.manage"
	})
	if !authors || teams {
		t.Fatalf("author-only reviewer resolved authors=%t teams=%t", authors, teams)
	}
}
