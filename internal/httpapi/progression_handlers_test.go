package httpapi

import (
	"strings"
	"testing"
)

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

func TestTaskConfigurationMapsRejectCorruptPersistedDefinitions(t *testing.T) {
	validCondition := []byte(`{"action":"create","objectType":"user","metric":"count","target":1}`)
	validRewards := []byte(`{"experience":1,"currencies":{}}`)
	for _, test := range []struct {
		name      string
		condition []byte
		rewards   []byte
		wantPart  string
	}{
		{name: "condition", condition: []byte(`{"target":"bad"}`), rewards: validRewards, wantPart: "condition"},
		{name: "rewards", condition: validCondition, rewards: []byte(`{}`), wantPart: "at least one"},
	} {
		t.Run(test.name, func(t *testing.T) {
			condition, rewards, err := decodeTaskConfigurationMaps(test.condition, test.rewards)
			if err == nil || condition != nil || rewards != nil || !strings.Contains(strings.ToLower(err.Error()), test.wantPart) {
				t.Fatalf("decoded=(%v,%v,%v); want nil/nil/error containing %q", condition, rewards, err, test.wantPart)
			}
		})
	}
}
