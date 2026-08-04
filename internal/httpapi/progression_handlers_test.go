package httpapi

import "testing"

func TestValidateTaskConditionSupportsCatalogResources(t *testing.T) {
	condition := map[string]any{
		"action":         " EDIT ",
		"objectType":     " RESOURCE ",
		"objectPublicId": "abc123xyz",
		"metric":         " COUNT ",
		"target":         float64(2),
	}
	if err := validateTaskCondition(condition); err != nil {
		t.Fatalf("validate resource task condition: %v", err)
	}
	if condition["action"] != "edit" || condition["objectType"] != "resource" || condition["metric"] != "count" {
		t.Fatalf("task condition was not normalized: %#v", condition)
	}
	if condition["target"] != int64(2) {
		t.Fatalf("task target was not normalized: %#v", condition["target"])
	}
}
