package httpapi

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestExportLootPoolsProjectVanillaRoles(t *testing.T) {
	// Shapes from the Minecraft 1.20.1 exporter, together with the older
	// keyed enchantment form. The public pool retains zero and false.
	const source = `[{
		"pool_index": 0,
		"conditions": [
			{"condition":"minecraft:inverted","term":{"condition":"minecraft:match_tool","predicate":{"enchantments":[{"enchantment":"minecraft:silk_touch","levels":{"min":1}}]}}},
			{"condition":"minecraft:table_bonus","chances":[0,0.5]},
			{"condition":"minecraft:location_check","predicate":{"block":{"blocks":["minecraft:large_fern"],"state":{"half":"upper"}}}},
			{"condition":"minecraft:damage_source_properties","predicate":{"tags":[{"id":"minecraft:is_lightning","expected":false}],"source_entity":{"type":"minecraft:frog","type_specific":{"type":"frog","variant":"minecraft:warm"}}}}
		],
		"entries":[{"entry_type":"minecraft:group","children":[
			{"entry_type":"minecraft:dynamic","dynamic_id":"minecraft:contents"},
			{"entry_type":"minecraft:tag","tag_id":"minecraft:saplings","expand":false}
		]}],
		"functions":[
			{"function":"minecraft:copy_nbt","source":"block_entity","ops":[{"source":"Bees","target":"BlockEntityTag.Bees","op":"replace"}]},
			{"function":"minecraft:enchant_randomly","enchantments":["minecraft:soul_speed"]},
			{"function":"minecraft:set_enchantments","enchantments":{"minecraft:fortune":2}},
			{"function":"minecraft:apply_bonus","parameters":{"bonusMultiplier":1,"extra":3,"probability":0.5}},
			{"function":"minecraft:copy_state","properties":["honey_level"]},
			{"function":"minecraft:exploration_map","decoration":"red_x","zoom":1,"skip_existing_chunks":false},
			{"function":"minecraft:set_potion","id":"minecraft:water"},
			{"function":"minecraft:set_instrument","options":"#minecraft:regular_goat_horns"}
		]
	}]`
	var pools []any
	if err := json.Unmarshal([]byte(source), &pools); err != nil {
		t.Fatal(err)
	}
	var expected []any
	if err := json.Unmarshal([]byte(source), &expected); err != nil {
		t.Fatal(err)
	}
	ops := pools[0].(map[string]any)["functions"].([]any)[0].(map[string]any)["ops"].([]any)
	ops[0].(map[string]any)["diagnostics"] = "local-only"
	got, err := registeredImportedCatalogJSONRule("pools").project(pools)
	if err != nil || !reflect.DeepEqual(got, expected) {
		t.Fatalf("projected pool differs from its registered fields: %v, %v", got, err)
	}
}

func TestExportLootPoolsRejectUnregisteredRoles(t *testing.T) {
	for _, tc := range []struct{ label, pool string }{
		{"unknown nested field", `{"conditions":[{"predicate":{"type_specific":{"variant":"minecraft:warm","extra_data":"value"}}}]}`},
		{"predicate enchantment list needs objects", `{"conditions":[{"predicate":{"enchantments":["minecraft:silk_touch"]}}]}`},
		{"function enchantment list needs identifiers", `{"functions":[{"enchantments":[{"enchantment":"minecraft:silk_touch"}]}]}`},
		{"probabilities must be numbers", `{"conditions":[{"chances":[false]}]}`},
		{"operations belong to functions", `{"entries":[{"ops":[]}]}`},
		{"copy operation vocabulary", `{"functions":[{"ops":[{"op":"other"}]}]}`},
		{"copy paths are structured", `{"functions":[{"ops":[{"source":"free form text"}]}]}`},
		{"flag must be boolean", `{"entries":[{"expand":0}]}`},
		{"entity variant must be a reference", `{"conditions":[{"predicate":{"type_specific":{"variant":"free form text"}}}]}`},
		{"source entity has a specific purpose", `{"conditions":[{"predicate":{"source_entity":{"text":"unrelated text"}}}]}`},
		{"property names must be identifiers", `{"functions":[{"properties":["free form text"]}]}`},
		{"potion is a resource ID", `{"functions":[{"id":"https://example.invalid/item"}]}`},
		{"instrument requires a tag", `{"functions":[{"options":"minecraft:regular_goat_horns"}]}`},
	} {
		t.Run(tc.label, func(t *testing.T) {
			var pool map[string]any
			if err := json.Unmarshal([]byte(tc.pool), &pool); err != nil {
				t.Fatal(err)
			}
			if _, err := registeredImportedCatalogJSONRule("pools").project([]any{pool}); !errors.Is(err, errExportJSONPurpose) {
				t.Fatalf("expected an unregistered shape error, got %v", err)
			}
		})
	}
}
