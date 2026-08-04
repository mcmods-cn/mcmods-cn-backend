package httpapi

import (
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestCanEditModAcceptsCanonicalScopedProjectPermission(t *testing.T) {
	identity := modIdentityRecord{UniqueID: "a2bc3de89"}
	tests := []struct {
		name        string
		permissions []string
		want        bool
	}{
		{
			name:        "global project edit",
			permissions: []string{"project.edit"},
			want:        true,
		},
		{
			name:        "canonical scoped project edit",
			permissions: []string{"project.edit.a2bc3de89"},
			want:        true,
		},
		{
			name:        "canonical scoped wildcard",
			permissions: []string{"project.edit.*"},
			want:        true,
		},
		{
			name:        "different project",
			permissions: []string{"project.edit.z9yx8wv76"},
			want:        false,
		},
		{
			name:        "legacy project editor remains compatible",
			permissions: []string{"project.editor.a2bc3de89"},
			want:        true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rules := make([]security.PermissionRule, 0, len(test.permissions))
			for _, permission := range test.permissions {
				rules = append(rules, security.PermissionRule{Code: permission, Allow: true})
			}
			claims := security.Claims{PermissionRules: rules}
			if got := canEditMod(claims, identity); got != test.want {
				t.Fatalf("canEditMod() = %v, want %v", got, test.want)
			}
		})
	}
}
