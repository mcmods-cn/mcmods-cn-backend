package httpapi

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNormalizeExportResourceKind(t *testing.T) {
	tests := map[string]string{
		"minecraft:item_stack":         "item",
		"forge:fluid_stack":            "fluid",
		"mekanism:gas_stack":           "gas",
		"pigment":                      "pigment",
		"slurry_stack":                 "slurry",
		"minecraft.loot_table":         "loot_table",
		"minecraft.enchantment":        "enchantment",
		"minecraft.dimension":          "dimension",
		"minecraft.advancement":        "advancement",
		"minecraft.natural_generation": "natural_generation",
	}
	for input, expected := range tests {
		if actual := normalizeExportResourceKind(input); actual != expected {
			t.Errorf("normalizeExportResourceKind(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestLocalizedExportResourceNames(t *testing.T) {
	names := map[string]string{
		"zh_cn": "Localized Water Vapor",
		"en_us": "Water Vapor",
		"ja_jp": "Localized Steam",
		"de_de": "Wasserdampf",
	}
	expected := map[string]string{"zh-CN": "Localized Water Vapor", "en-US": "Water Vapor", "ja-JP": "Localized Steam"}
	if actual := localizedExportResourceNames(names, "ja-JP", "zh_cn"); !reflect.DeepEqual(actual, expected) {
		t.Fatalf("localizedExportResourceNames() = %#v, want %#v", actual, expected)
	}
	if actual := localizedExportResourceNames(names, "fr_fr"); !reflect.DeepEqual(actual, map[string]string{"zh-CN": "Localized Water Vapor", "en-US": "Water Vapor"}) {
		t.Fatalf("localizedExportResourceNames() fallback = %#v", actual)
	}
}

func TestLootTableItemIDs(t *testing.T) {
	data := map[string]any{
		"possible_item_ids": []any{"minecraft:diamond", "example:part"},
		"definition": map[string]any{
			"pools": []any{
				map[string]any{"entries": []any{
					map[string]any{"type": "minecraft:item", "name": "othermod:material"},
					map[string]any{"type": "minecraft:item", "name": "minecraft:diamond"},
				}},
			},
		},
	}
	expected := []string{"minecraft:diamond", "example:part", "othermod:material"}
	if actual := lootTableItemIDs(data); !reflect.DeepEqual(actual, expected) {
		t.Fatalf("lootTableItemIDs() = %#v, want %#v", actual, expected)
	}
}

func TestLootTableIconPreviewsPreservesPossibleItemOrder(t *testing.T) {
	data := map[string]any{
		"possible_item_ids": []any{"minecraft:diamond", "minecraft:stick", "example:missing"},
		"resourceSources": map[string]any{
			"minecraft:diamond": map[string]any{"sourceRevisionId": "revision-a", "iconPath": "items/diamond.png"},
			"minecraft:stick":   map[string]any{"sourceRevisionId": "revision-b", "iconPath": "items/stick.png"},
			"example:missing":   map[string]any{"sourceRevisionId": "revision-c"},
		},
	}
	expected := []map[string]string{
		{"sourceRevisionId": "revision-a", "iconPath": "items/diamond.png"},
		{"sourceRevisionId": "revision-b", "iconPath": "items/stick.png"},
	}
	if actual := lootTableIconPreviews(data); !reflect.DeepEqual(actual, expected) {
		t.Fatalf("lootTableIconPreviews() = %#v, want %#v", actual, expected)
	}
}

func TestCompatibleEnchantmentIDs(t *testing.T) {
	data := map[string]any{
		"enchanting": map[string]any{
			"compatible_enchantments": []any{
				"minecraft:unbreaking",
				"minecraft:mending",
				"minecraft:unbreaking",
				"",
			},
		},
	}
	expected := []string{"minecraft:unbreaking", "minecraft:mending"}
	if actual := compatibleEnchantmentIDs(data); !reflect.DeepEqual(actual, expected) {
		t.Fatalf("compatibleEnchantmentIDs() = %#v, want %#v", actual, expected)
	}
}

func TestLootTableReferenceIDs(t *testing.T) {
	data := map[string]any{
		"loot_table":         "minecraft:blocks/stone",
		"default_loot_table": "minecraft:entities/cow",
		"referenced_loot_tables": []any{
			"minecraft:gameplay/fishing",
			"minecraft:blocks/stone",
		},
		"definition": map[string]any{
			"pools": []any{map[string]any{
				"entries": []any{map[string]any{
					"type": "minecraft:loot_table",
					"name": "example:nested/reward",
				}},
			}},
		},
	}
	expected := []string{
		"minecraft:blocks/stone",
		"minecraft:entities/cow",
		"minecraft:gameplay/fishing",
		"example:nested/reward",
	}
	if actual := lootTableReferenceIDs(data); !reflect.DeepEqual(actual, expected) {
		t.Fatalf("lootTableReferenceIDs() = %#v, want %#v", actual, expected)
	}
}

func TestModExportEntryDetailPreservesFullSnapshotData(t *testing.T) {
	raw := []byte(`{
		"category": "blocks",
		"definition": {
			"type": "minecraft:block",
			"pools": [{
				"rolls": 1,
				"entries": [{
					"type": "minecraft:item",
					"name": "minecraft:diamond"
				}]
			}]
		},
		"possible_item_ids": ["minecraft:diamond"]
	}`)
	data, err := decodeModExportEntryData(raw)
	if err != nil {
		t.Fatal(err)
	}
	definition, ok := data["definition"].(map[string]any)
	if !ok {
		t.Fatalf("definition = %#v, want object", data["definition"])
	}
	pools, ok := definition["pools"].([]any)
	if !ok || len(pools) != 1 {
		t.Fatalf("definition.pools = %#v, want one full pool", definition["pools"])
	}
	responseRaw, err := json.Marshal(modExportEntryDetailResponse{
		PublicID: "res_123456789",
		Data:     data,
	})
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err = json.Unmarshal(responseRaw, &response); err != nil {
		t.Fatal(err)
	}
	if response["publicId"] != "res_123456789" {
		t.Fatalf("response publicId = %#v", response["publicId"])
	}
	if _, exists := response["entityId"]; exists {
		t.Fatalf("response retained duplicate entityId: %#v", response)
	}
	encodedData, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatalf("response data = %#v, want object", response["data"])
	}
	encodedDefinition, ok := encodedData["definition"].(map[string]any)
	if !ok {
		t.Fatalf("response data.definition = %#v, want object", encodedData["definition"])
	}
	encodedPools, ok := encodedDefinition["pools"].([]any)
	if !ok || len(encodedPools) != 1 {
		t.Fatalf("response data.definition.pools = %#v, want one full pool", encodedDefinition["pools"])
	}
}

func TestModExportEntryDetailDoesNotFabricateMissingLootDefinition(t *testing.T) {
	data, err := decodeModExportEntryData([]byte(`{
		"category": "archaeology",
		"runtime_loaded": true
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := data["definition"]; exists {
		t.Fatalf("definition = %#v, want absent when the exporter did not provide it", data["definition"])
	}
}
