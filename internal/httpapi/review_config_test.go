package httpapi

import "testing"

func TestSimpleProjectReviewRequiredUsesIndependentSettings(t *testing.T) {
	config := reviewConfig{
		PluginCreate: true,
		MapEdit:      true,
		AddonCreate:  true,
	}
	tests := []struct {
		projectType string
		action      string
		want        bool
	}{
		{projectType: "plugin", action: "create", want: true},
		{projectType: "plugin", action: "edit", want: false},
		{projectType: "map", action: "create", want: false},
		{projectType: "map", action: "edit", want: true},
		{projectType: "addon", action: "create", want: true},
		{projectType: "addon", action: "edit", want: false},
		{projectType: "unknown", action: "create", want: true},
	}
	for _, test := range tests {
		if got := simpleProjectReviewRequired(config, test.projectType, test.action); got != test.want {
			t.Fatalf("review required for %s %s = %v, want %v", test.projectType, test.action, got, test.want)
		}
	}
}
