package httpapi

import "testing"

func TestResolveCommentProjectRole(t *testing.T) {
	tests := []struct {
		name    string
		signals commentProjectRoleSignals
		want    string
	}{
		{
			name:    "project creator is owner",
			signals: commentProjectRoleSignals{ProjectCreator: true},
			want:    "owner",
		},
		{
			name:    "configured developer role is owner",
			signals: commentProjectRoleSignals{ConfiguredDeveloperRole: true},
			want:    "owner",
		},
		{
			name:    "canonical scoped edit permission is editor",
			signals: commentProjectRoleSignals{ScopedEditorPermission: true},
			want:    "editor",
		},
		{
			name: "owner takes precedence over editor",
			signals: commentProjectRoleSignals{
				DeveloperMembership: true,
				EditorMembership:    true,
			},
			want: "owner",
		},
		{
			name: "unrelated user has no project badge",
			want: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveCommentProjectRole(test.signals); got != test.want {
				t.Fatalf("resolveCommentProjectRole() = %q, want %q", got, test.want)
			}
		})
	}
}
