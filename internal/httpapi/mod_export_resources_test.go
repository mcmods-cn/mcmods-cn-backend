package httpapi

import (
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
	expected := map[string]string{"zh_cn": "Localized Water Vapor", "en_us": "Water Vapor", "ja_jp": "Localized Steam"}
	if actual := localizedExportResourceNames(names, "ja-JP", "zh_cn"); !reflect.DeepEqual(actual, expected) {
		t.Fatalf("localizedExportResourceNames() = %#v, want %#v", actual, expected)
	}
	if actual := localizedExportResourceNames(names, "fr_fr"); !reflect.DeepEqual(actual, map[string]string{"zh_cn": "Localized Water Vapor", "en_us": "Water Vapor"}) {
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
