package httpapi

import (
	"encoding/json"
	"fmt"
)

func decodeStoredJSONObject(raw []byte, label string) (map[string]any, error) {
	var value map[string]any
	if len(raw) == 0 {
		return nil, fmt.Errorf("%s is empty", label)
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("decode %s: %w", label, err)
	}
	if value == nil {
		return nil, fmt.Errorf("%s must be a JSON object", label)
	}
	return value, nil
}
