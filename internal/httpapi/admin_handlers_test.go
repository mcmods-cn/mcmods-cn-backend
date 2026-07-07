package httpapi

import "testing"

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
}
