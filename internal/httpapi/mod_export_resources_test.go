package httpapi

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNormalizeExportResourceKind(t *testing.T) {
	tests := map[string]string{
		"minecraft:item_stack": "item",
		"forge:fluid_stack":    "fluid",
		"mekanism:gas_stack":   "gas",
		"pigment":              "pigment",
		"slurry_stack":         "slurry",
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
		EntityID: "res_123456789",
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
