package httpapi

import (
	"mcmods-cn-backend/internal/security"
	"testing"
)

func TestAutomationScopedPermissionUsesCurrentEffectiveDenyAndScope(t *testing.T) {
	for _, test := range []struct {
		name  string
		rules []security.PermissionRule
		want  bool
	}{
		{"global grant", []security.PermissionRule{{Code: "project.edit", Allow: true, Priority: 100}}, true},
		{"exact scope", []security.PermissionRule{{Code: "project.edit.target001", Allow: true, Priority: 100}}, true},
		{"other scope", []security.PermissionRule{{Code: "project.edit.other0001", Allow: true, Priority: 100}}, false},
		{"scoped explicit deny", []security.PermissionRule{{Code: "project.edit", Allow: true, Priority: 100}, {Code: "project.edit.target001", Allow: false, Priority: 100}}, false},
		{"wildcard explicit deny", []security.PermissionRule{{Code: "project.edit", Allow: true, Priority: 100}, {Code: "project.edit.*", Allow: false, Priority: 200}}, false},
		{"no fallback", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := automationScopedPermission(security.Claims{PermissionRules: test.rules}, "project.edit", "target001"); got != test.want {
				t.Fatalf("allowed=%v want %v", got, test.want)
			}
		})
	}
}
