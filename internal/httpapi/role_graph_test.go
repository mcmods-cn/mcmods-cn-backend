package httpapi

import "testing"

func TestValidateRoleGraphRejectsMissingParentsAndCycles(t *testing.T) {
	for name, graph := range map[string]map[string][]string{
		"missing": {"admin": {"does_not_exist"}},
		"cycle":   {"a": {"b"}, "b": {"c"}, "c": {"a"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateRoleGraph(graph); err == nil {
				t.Fatalf("invalid role graph was accepted: %#v", graph)
			}
		})
	}
	if err := validateRoleGraph(map[string][]string{
		"registered": {}, "moderator": {"registered"}, "admin": {"moderator"},
	}); err != nil {
		t.Fatalf("valid role graph was rejected: %v", err)
	}
}
