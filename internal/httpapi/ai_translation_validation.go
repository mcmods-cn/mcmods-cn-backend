package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// These are structural checks, not a claim about linguistic translation quality.
// Tokens that carry executable or navigational meaning must survive translation.
var aiProtectedTokens = regexp.MustCompile("(?s)```.*?```|`[^`\\n]+`|\\{\\{?[A-Za-z_][A-Za-z0-9_.-]*\\}?\\}|https?://[^\\s<>\\)\\]]+")

func validateAITranslationResult(taskType string, rawPayload []byte, result map[string]any) error {
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil || len(payload.Items) == 0 || len(payload.Items) > 100 {
		return errors.New("AI translation source items are invalid")
	}
	fields := []string{"text"}
	if taskType == aiTaskPermissionTranslation {
		fields = []string{"name", "description"}
	}
	sources := make(map[string]map[string]any, len(payload.Items))
	for _, source := range payload.Items {
		key, ok := source["key"].(string)
		if !ok || strings.TrimSpace(key) == "" {
			return errors.New("AI translation source key is invalid")
		}
		if _, duplicate := sources[key]; duplicate {
			return errors.New("AI translation source contains duplicate keys")
		}
		for _, field := range fields {
			if _, ok := source[field].(string); !ok {
				return fmt.Errorf("AI translation source field %q is invalid", field)
			}
		}
		sources[key] = source
	}
	items, ok := result["items"].([]any)
	if !ok || len(result) != 1 || len(items) != len(sources) {
		return errors.New("AI translation result does not match the source item count")
	}
	seen := make(map[string]bool, len(items))
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok || len(item) != len(fields)+1 {
			return errors.New("AI translation result has unexpected fields")
		}
		key, ok := item["key"].(string)
		source, exists := sources[key]
		if !ok || !exists || seen[key] {
			return errors.New("AI translation result has an unknown or duplicate key")
		}
		seen[key] = true
		for _, field := range fields {
			text, ok := item[field].(string)
			original := source[field].(string)
			if !ok || (strings.TrimSpace(original) != "" && strings.TrimSpace(text) == "") {
				return fmt.Errorf("AI translation result field %q is missing or empty", field)
			}
			if !sameAIProtectedTokens(original, text) {
				return fmt.Errorf("AI translation result field %q damaged placeholders, code, or URLs", field)
			}
		}
	}
	return nil
}

func sameAIProtectedTokens(source, translated string) bool {
	counts := make(map[string]int)
	for _, token := range aiProtectedTokens.FindAllString(source, -1) {
		counts[token]++
	}
	for _, token := range aiProtectedTokens.FindAllString(translated, -1) {
		counts[token]--
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func decodeUniqueAIJSON(content string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(content))
	value, err := decodeUniqueAIJSONValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected content after the translation JSON")
	}
	return value, nil
}

func decodeUniqueAIJSONValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, errors.New("translation JSON nesting is too deep")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := make(map[string]any)
		for decoder.More() {
			token, err = decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := token.(string)
			if !ok {
				return nil, errors.New("translation JSON object key is invalid")
			}
			if _, exists := object[key]; exists {
				return nil, errors.New("translation JSON contains a duplicate field")
			}
			value, err := decodeUniqueAIJSONValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
			return nil, errors.New("translation JSON object is incomplete")
		}
		return object, nil
	case json.Delim('['):
		array := make([]any, 0)
		for decoder.More() {
			value, err := decodeUniqueAIJSONValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if token, err = decoder.Token(); err != nil || token != json.Delim(']') {
			return nil, errors.New("translation JSON array is incomplete")
		}
		return array, nil
	default:
		if _, delim := token.(json.Delim); delim {
			return nil, errors.New("unexpected translation JSON delimiter")
		}
		return token, nil
	}
}

func aiTranslationReservationForPayload(taskType string, binding aiTaskModelConfig, model aiModelConfig, rawPayload []byte) (int64, error) {
	prompt, err := buildTranslationPrompt(taskType, binding.Prompt, rawPayload)
	if err != nil {
		return 0, err
	}
	_, reserved, err := aiTranslationTokenReservation(model, prompt)
	return reserved, err
}
