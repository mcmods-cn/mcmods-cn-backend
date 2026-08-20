package httpapi

import (
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestCommentProjectRoleFromPermissions(t *testing.T) {
	tests := []struct {
		name        string
		permissions []security.PermissionRule
		want        string
	}{
		{
			name:        "developer badge uses its dedicated project permission",
			permissions: []security.PermissionRule{{Code: "project.comment.role.developer.demo12345", Allow: true, Priority: 100}},
			want:        "developer",
		},
		{
			name:        "editor badge uses its dedicated project permission",
			permissions: []security.PermissionRule{{Code: "project.comment.role.editor.demo12345", Allow: true, Priority: 100}},
			want:        "editor",
		},
		{
			name: "developer takes precedence over editor",
			permissions: []security.PermissionRule{
				{Code: "project.comment.role.developer.demo12345", Allow: true, Priority: 100},
				{Code: "project.comment.role.editor.demo12345", Allow: true, Priority: 100},
			},
			want: "developer",
		},
		{
			name: "explicit higher priority deny hides badge",
			permissions: []security.PermissionRule{
				{Code: "project.comment.role.developer.demo12345", Allow: true, Priority: 100},
				{Code: "project.comment.role.developer.demo12345", Allow: false, Priority: 200},
			},
			want: "",
		},
		{
			name:        "project edit permission does not imply a badge",
			permissions: []security.PermissionRule{{Code: "project.edit.demo12345", Allow: true, Priority: 100}},
			want:        "",
		},
		{
			name:        "admin wildcard does not claim project developer identity",
			permissions: []security.PermissionRule{{Code: "admin.*", Allow: true, Priority: 100}},
			want:        "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := commentProjectRoleFromPermissions(test.permissions, "demo12345"); got != test.want {
				t.Fatalf("commentProjectRoleFromPermissions() = %q, want %q", got, test.want)
			}
		})
	}
}
