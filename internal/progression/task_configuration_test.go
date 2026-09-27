package progression

import (
	"strings"
	"testing"
)

func TestTaskConfigurationRejectsInvalidConditionsAndRewards(t *testing.T) {
	validCondition := `{"action":"create","objectType":"user","metric":"count","target":1}`
	validRewards := `{"experience":10,"currencies":{"gold_nugget":2}}`
	tests := []struct {
		name      string
		condition string
		rewards   string
		wantPart  string
	}{
		{name: "condition type", condition: `{"action":"create","objectType":"user","metric":"count","target":"one"}`, rewards: validRewards, wantPart: "condition"},
		{name: "condition target", condition: `{"action":"create","objectType":"user","metric":"count","target":0}`, rewards: validRewards, wantPart: "target"},
		{name: "condition action", condition: `{"action":"unknown","objectType":"user","metric":"count","target":1}`, rewards: validRewards, wantPart: "action"},
		{name: "condition object", condition: `{"action":"create","objectType":"unknown","metric":"count","target":1}`, rewards: validRewards, wantPart: "object"},
		{name: "condition metric", condition: `{"action":"create","objectType":"user","metric":"sum","target":1}`, rewards: validRewards, wantPart: "metric"},
		{name: "rewards type", condition: validCondition, rewards: `{"experience":"ten"}`, wantPart: "rewards"},
		{name: "empty rewards", condition: validCondition, rewards: `{}`, wantPart: "at least one"},
		{name: "negative experience", condition: validCondition, rewards: `{"experience":-1}`, wantPart: "experience"},
		{name: "zero currency", condition: validCondition, rewards: `{"currencies":{"gold_nugget":0}}`, wantPart: "currency"},
		{name: "unnormalized currency", condition: validCondition, rewards: `{"currencies":{" Gold_Nugget ":1}}`, wantPart: "currency"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := decodeTaskConfiguration([]byte(test.condition), []byte(test.rewards))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.wantPart) {
				t.Fatalf("decode error=%v; want an error containing %q", err, test.wantPart)
			}
		})
	}
}

func TestTaskConfigurationAcceptsValidatedConfiguration(t *testing.T) {
	condition, rewards, err := decodeTaskConfiguration(
		[]byte(`{"action":"create","objectType":"user","objectPublicId":"abc123","metric":"count","target":2}`),
		[]byte(`{"experience":10,"currencies":{"gold_nugget":2}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if condition.Target != 2 || condition.ObjectPublicID != "abc123" || rewards.Experience != 10 || rewards.Currencies["gold_nugget"] != 2 {
		t.Fatalf("decoded configuration=(%#v,%#v)", condition, rewards)
	}
}
