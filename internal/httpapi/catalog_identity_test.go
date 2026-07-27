package httpapi

import "testing"

func TestResourceKindForNamespacedTagRegistry(t *testing.T) {
	tests := map[string]string{
		"minecraft:item":        "minecraft.item",
		"minecraft:block":       "minecraft.block",
		"minecraft:fluid":       "minecraft.fluid",
		"minecraft:entity_type": "minecraft.entity_type",
		"minecraft:mob_effect":  "minecraft.mob_effect",
		"loot_tables":           "minecraft.loot_table",
		"game_settings":         "minecraft.game_setting",
		"game_rules":            "minecraft.game_setting",
	}
	for registry, expected := range tests {
		if actual := resourceKindForRegistry(registry); actual != expected {
			t.Fatalf("resourceKindForRegistry(%q) = %q, want %q", registry, actual, expected)
		}
	}
}
