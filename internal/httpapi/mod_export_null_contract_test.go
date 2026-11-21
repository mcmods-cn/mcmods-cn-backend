package httpapi

import "testing"

func TestImportedCodecsRequirePresentValuesAtRegisteredLocations(t *testing.T) {
	flat := registeredImportedCatalogJSONRule("generatorSettings")
	display := exportDisplayJSONRule()
	placement := registeredImportedCatalogJSONRule("placement")
	for _, tc := range []struct {
		name  string
		rule  *exportJSONRule
		value any
		valid bool
	}{
		{"absent icon tag", display, map[string]any{"icon": map[string]any{"item": "minecraft:stick"}}, true},
		{"empty raw tag", display, map[string]any{"icon": map[string]any{"nbt_snbt": ""}}, true},
		{"false tag presence", display, map[string]any{"icon": map[string]any{"has_stack_tag": false}}, true},
		{"null raw tag", display, map[string]any{"icon": map[string]any{"nbt_snbt": nil}}, false},
		{"null tag marker", display, map[string]any{"icon": map[string]any{"has_stack_tag": nil}}, false},
		{"null structure overrides", flat, map[string]any{"structure_overrides": nil}, false},
		{"null structure ID", flat, map[string]any{"structure_overrides": []any{nil}}, false},
		{"no structure overrides", flat, map[string]any{"structure_overrides": []any{}}, true},
		{"null placement", placement, nil, false},
		{"null count", placement, map[string]any{"count": nil}, false},
		{"null modifier", placement, map[string]any{"modifiers": []any{nil}}, false},
		{"null offset component", placement, map[string]any{"modifiers": []any{map[string]any{"predicate": map[string]any{"offset": []any{float64(0), nil, float64(0)}}}}}, false},
		{"null referenced modifier", naturalGenerationRules.placedFeature, map[string]any{"placement": []any{nil}}, false},
		{"zero placement", placement, map[string]any{"count": float64(0), "modifiers": []any{}, "biome_filter": false}, true},
		{"empty vanilla advancement rewards", registeredImportedCatalogJSONRule("rewards"), nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.rule.project(tc.value)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, want %v", err == nil, tc.valid)
			}
		})
	}
}
