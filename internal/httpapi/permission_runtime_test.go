package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcmods-cn-backend/internal/domain"
	"mcmods-cn-backend/internal/security"
)

func TestPermissionComparisonMetadataResolvesExactAndMostSpecificTemplate(t *testing.T) {
	catalog := []domain.Permission{
		{Code: "project.[action].<object>", Name: "Generic project action"},
		{Code: "project.edit.<ProjectID>", Name: "Edit project", Description: "Scoped"},
		{Code: "project.edit.[otherID]", Name: "Equivalent later template"},
		{Code: "project.edit.known", Name: "Known project"},
		{Code: "user.file.daily_limit.<num>", Name: "Daily upload limit"},
	}
	rows := []permissionComparisonRow{
		{Code: "project.edit.112345"}, {Code: "project.edit.*"}, {Code: "project.edit.known"},
		{Code: "user.file.daily_limit.150"}, {Code: "project.edit.<ProjectID>"}, {Code: "absent"},
	}
	decoratePermissionComparisonRows(rows, catalog)
	want := []string{"Edit project", "Edit project", "Known project", "Daily upload limit", "Edit project", ""}
	for i, row := range rows {
		if row.Name != want[i] {
			t.Errorf("permission %q name=%q want=%q", row.Code, row.Name, want[i])
		}
	}
	if rows[0].Description != "Scoped" || rows[5].Translations == nil {
		t.Fatalf("description or empty translation map lost: %+v", rows)
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

func TestOSSQuotaHonorsMoreSpecificLimitWithinAdministrativeGrant(t *testing.T) {
	claims := security.Claims{PermissionRules: []security.PermissionRule{
		{Code: "admin.*", Allow: true, Priority: 10},
		{Code: "user.file.single_limit.*", Allow: false, Priority: 20},
		{Code: "user.file.single_limit.1", Allow: true, Priority: 30},
	}}
	req := httptest.NewRequest("POST", "/api/v1/users/me/oss/uploads", nil)
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
	limits, err := ossUserQuotaLimitsForRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if limits.daily != maxPermissionBytes || limits.total != maxPermissionBytes || limits.validateSingle(1<<20) != nil || limits.validateSingle(1<<20+1) == nil {
		t.Fatalf("storage limits did not retain the configured one-MiB allowance: %+v", limits)
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

func TestInheritedPermissionUsesNearestConcreteRoleAcrossRoots(t *testing.T) {
	const permission = "project.edit.alpha"
	graph := map[string]permissionRole{
		"team":       {Parents: []string{"reviewer", "maintainer"}},
		"reviewer":   {Parents: []string{"editor.alpha", "standard"}},
		"maintainer": {Parents: []string{"reviewer", "editor.beta"}},
		"standard":   {Weight: 10, Permissions: []domain.RolePermissionEntry{{Code: permission, Allow: false}}},
	}
	template := permissionRole{Code: "editor.[project]", Weight: 10, hasExpiringRules: true,
		Permissions: []domain.RolePermissionEntry{{Code: "project.edit.[project]", Allow: true}}}
	for _, assigned := range [][]string{{"team", "editor.alpha"}, {"editor.alpha", "team"}} {
		rules := map[string]permissionCandidate{}
		temporary := applyPermissionRoles(rules, assigned, graph, []permissionRole{template})
		alpha, beta := rules[permission], rules["project.edit.beta"]
		if !temporary || len(rules) != 2 || !alpha.Allow || alpha.Depth != 0 || alpha.Source != "group.editor.alpha" || !beta.Allow || beta.Depth != 2 {
			t.Fatalf("roots %v result=%+v temporary=%t", assigned, rules, temporary)
		}
	}
}

func BenchmarkPermissionRoleSharedParents(b *testing.B) {
	graph := map[string]permissionRole{"base": {Permissions: []domain.RolePermissionEntry{{Code: "news.read", Allow: true}}}}
	parents := []string{"base"}
	for level := 0; level < 8; level++ {
		next := []string{fmt.Sprintf("editor_%d", level), fmt.Sprintf("author_%d", level)}
		for _, name := range next {
			graph[name] = permissionRole{Parents: parents, Permissions: []domain.RolePermissionEntry{{Code: "news.read", Allow: true}}}
		}
		parents = next
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		candidates := map[string]permissionCandidate{}
		applyPermissionRoles(candidates, parents, graph, nil)
		if !candidates["news.read"].Allow {
			b.Fatal("grant was lost")
		}
	}
}

func TestPermissionEvaluationDistinguishesZeroQuotaFromParentBooleanIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	actor := f.editor
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[actor], "admin.*", "user.file.total_limit.*", "admin.users.*")
	if _, err := f.db.Exec(f.ctx, `update user_permissions assignment set allow=false from permissions node
		where assignment.permission_id=node.id and assignment.user_id=$1 and node.code in ('user.file.total_limit.*','admin.users.*')`, f.userIDs[actor]); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		code    string
		allowed bool
	}{{"user.file.total_limit", false}, {"admin.users", true}} {
		response := decodeTEST022Data[struct {
			Permission string
			Allowed    *bool
			Value      *int32
		}](t,
			f.require(t, actor, http.MethodGet, "/api/v1/auth/permissions/evaluate?code="+test.code, nil, http.StatusOK))
		if response.Permission != test.code || response.Allowed == nil || *response.Allowed != test.allowed || response.Value == nil || *response.Value != 0 {
			t.Fatalf("permission %s returned %+v", test.code, response)
		}
	}
}
