package progression

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"mcmods-cn-backend/internal/activitycatalog"
)

// ValidateTaskConfiguration checks the persisted condition and reward
// documents with the same strict semantics used by the activity projector.
// It is exported so HTTP read paths cannot present corrupt task definitions as
// valid empty objects.
func ValidateTaskConfiguration(conditionRaw, rewardsRaw []byte) error {
	_, _, err := decodeTaskConfiguration(conditionRaw, rewardsRaw)
	return err
}

func decodeTaskConfiguration(conditionRaw, rewardsRaw []byte) (taskCondition, taskRewards, error) {
	var condition taskCondition
	if err := json.Unmarshal(conditionRaw, &condition); err != nil {
		return taskCondition{}, taskRewards{}, fmt.Errorf("decode task condition: %w", err)
	}
	if condition.Action != normalizedTaskCode(condition.Action) || activitycatalog.ActionID(condition.Action) == 0 {
		return taskCondition{}, taskRewards{}, errors.New("task condition action is invalid")
	}
	if condition.ObjectType != normalizedTaskCode(condition.ObjectType) || activitycatalog.ObjectTypeID(condition.ObjectType) == 0 {
		return taskCondition{}, taskRewards{}, errors.New("task condition object type is invalid")
	}
	if condition.ObjectPublicID != strings.TrimSpace(condition.ObjectPublicID) {
		return taskCondition{}, taskRewards{}, errors.New("task condition object public ID is invalid")
	}
	if condition.Metric != "count" && condition.Metric != "markdown_bytes" {
		return taskCondition{}, taskRewards{}, errors.New("task condition metric is invalid")
	}
	if condition.Target <= 0 {
		return taskCondition{}, taskRewards{}, errors.New("task condition target must be positive")
	}

	var rewards taskRewards
	if err := json.Unmarshal(rewardsRaw, &rewards); err != nil {
		return taskCondition{}, taskRewards{}, fmt.Errorf("decode task rewards: %w", err)
	}
	if rewards.Experience < 0 {
		return taskCondition{}, taskRewards{}, errors.New("task reward experience cannot be negative")
	}
	for code, amount := range rewards.Currencies {
		if code == "" || code != normalizedTaskCode(code) || amount <= 0 {
			return taskCondition{}, taskRewards{}, fmt.Errorf("task currency reward %q is invalid", code)
		}
	}
	if rewards.Experience == 0 && len(rewards.Currencies) == 0 {
		return taskCondition{}, taskRewards{}, errors.New("task must provide at least one reward")
	}
	return condition, rewards, nil
}

func normalizedTaskCode(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
