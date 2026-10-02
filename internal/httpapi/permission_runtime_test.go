package httpapi

import (
	"testing"

	"mcmods-cn-backend/internal/domain"
	"mcmods-cn-backend/internal/security"
)

func TestMatchPermissionCatalogUsesVariableTemplate(t *testing.T) {
	catalog := []domain.Permission{
		{Code: "project.edit.<ProjectID>", Name: "Edit project"},
		{Code: "user.file.daily_limit.<num>", Name: "Daily upload limit"},
	}
	for code, expected := range map[string]string{
		"project.edit.112345":       "Edit project",
		"project.edit.*":            "Edit project",
		"user.file.daily_limit.150": "Daily upload limit",
	} {
		permission := matchPermissionCatalog(code, catalog)
		if permission == nil || permission.Name != expected {
			t.Fatalf("matchPermissionCatalog(%q) = %#v, want %q", code, permission, expected)
		}
	}
}

func TestPermissionRulesAllowDirectDenyOverridesRoleGrant(t *testing.T) {
	entries := []security.PermissionRule{
		{Code: "user.message.receive", Allow: true, Priority: 10, Source: "group.member"},
		{Code: "user.message.receive", Allow: false, Priority: directUserPermissionPriority, Source: "user"},
	}
	if permissionRulesAllow(entries, "user.message.receive") {
		t.Fatal("expected direct deny to override role grant")
	}
}

func TestMergePermissionComparisonRows(t *testing.T) {
	left := []security.PermissionRule{{Code: "a.read", Allow: true}, {Code: "shared", Allow: true}}
	right := []security.PermissionRule{{Code: "b.read", Allow: true}, {Code: "shared", Allow: false}}
	rows := mergePermissionComparisonRows(left, right)
	if len(rows) != 3 || rows[0].Code != "a.read" || rows[1].Code != "b.read" || rows[2].Code != "shared" {
		t.Fatalf("unexpected comparison rows: %#v", rows)
	}
	if rows[0].Left == nil || rows[0].Right != nil {
		t.Fatalf("expected a.read to exist only on the left: %#v", rows[0])
	}
	if rows[2].Left == nil || rows[2].Right == nil || rows[2].Left.Allow == rows[2].Right.Allow {
		t.Fatalf("expected shared permission values to differ: %#v", rows[2])
	}
}

func TestPermissionRulesAllowSpecificGrantOverridesWildcardAtSamePriority(t *testing.T) {
	entries := []security.PermissionRule{
		{Code: "user.message.*", Allow: false, Priority: 10},
		{Code: "user.message.receive", Allow: true, Priority: 10},
	}
	if !permissionRulesAllow(entries, "user.message.receive") {
		t.Fatal("expected the more specific permission to win")
	}
}

func TestPermissionRulesAllowHigherPriorityWildcardDenyWins(t *testing.T) {
	entries := []security.PermissionRule{
		{Code: "project.edit.abc123456", Allow: true, Priority: 10},
		{Code: "project.edit.*", Allow: false, Priority: 20},
	}
	if permissionRulesAllow(entries, "project.edit.abc123456") {
		t.Fatal("expected the higher-priority wildcard deny to win")
	}
}

func TestPermissionRulesNumericValueHonorsDeny(t *testing.T) {
	entries := []security.PermissionRule{
		{Code: "user.file.total_limit.500", Allow: true, Priority: 10},
		{Code: "user.file.total_limit.*", Allow: false, Priority: 20},
		{Code: "user.file.total_limit.100", Allow: true, Priority: 30},
	}
	if got := permissionRulesNumericValue(entries, "user.file.total_limit"); got != 100 {
		t.Fatalf("permissionRulesNumericValue() = %d, want 100", got)
	}
}

func TestClaimsAllowHonorsResolvedDeny(t *testing.T) {
	claims := security.Claims{
		PermissionRules: []security.PermissionRule{
			{Code: "admin.*", Allow: false, Priority: directUserPermissionPriority},
		},
	}
	if claimsAllow(claims, "admin.users.read") {
		t.Fatal("resolved deny rule must not authorize a request")
	}
}
