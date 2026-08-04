package httpapi

import (
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestMatchRoleTemplateCode(t *testing.T) {
	tests := []struct {
		name     string
		template string
		role     string
		want     map[string]string
		ok       bool
	}{
		{
			name:     "single variable",
			template: "project_editor.[ProjectID]",
			role:     "project_editor.112345",
			want:     map[string]string{"ProjectID": "112345"},
			ok:       true,
		},
		{
			name:     "multiple variables",
			template: "project_editor.[ProjectID].[ClassID]",
			role:     "project_editor.112345.7",
			want:     map[string]string{"ProjectID": "112345", "ClassID": "7"},
			ok:       true,
		},
		{
			name:     "wildcard variable",
			template: "project_editor.[ProjectID]",
			role:     "project_editor.*",
			want:     map[string]string{"ProjectID": "*"},
			ok:       true,
		},
		{
			name:     "repeated variable mismatch",
			template: "project_editor.[ProjectID].[ProjectID]",
			role:     "project_editor.112345.7",
			ok:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := matchRoleTemplateCode(tt.template, tt.role)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			for key, want := range tt.want {
				if got[key] != want {
					t.Fatalf("variable %s = %q, want %q", key, got[key], want)
				}
			}
		})
	}
}

func TestApplyRoleVariables(t *testing.T) {
	variables := map[string]string{"ProjectID": "112345", "ClassID": "*"}
	got := applyRoleVariables("project.edit.[ProjectID].[ClassID]", variables)
	if got != "project.edit.112345.*" {
		t.Fatalf("expanded permission = %q", got)
	}
	got = applyRoleVariables("project.edit.<projectID>", variables)
	if got != "project.edit.112345" {
		t.Fatalf("case-insensitive variable expansion = %q", got)
	}
}

func TestPermissionCandidatePriority(t *testing.T) {
	candidates := map[string]permissionCandidate{}
	applyPermissionCandidate(candidates, permissionCandidate{
		PermissionRule: security.PermissionRule{Code: "project.edit.*", Allow: true, Priority: 10, Source: "group.editor"},
		Depth:          0,
	})
	applyPermissionCandidate(candidates, permissionCandidate{
		PermissionRule: security.PermissionRule{Code: "project.edit.*", Allow: false, Priority: 10, Source: "group.restricted"},
		Depth:          0,
	})
	if candidates["project.edit.*"].Allow {
		t.Fatal("deny must win when role priority and depth are equal")
	}

	applyPermissionCandidate(candidates, permissionCandidate{
		PermissionRule: security.PermissionRule{Code: "project.edit.*", Allow: true, Priority: directUserPermissionPriority, Source: "user"},
		Depth:          -1,
	})
	got := candidates["project.edit.*"]
	if !got.Allow || got.Source != "user" {
		t.Fatalf("direct user permission did not override role permission: %#v", got)
	}
}
