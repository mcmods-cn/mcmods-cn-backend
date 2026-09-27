package httpapi

import (
	"strings"
	"testing"
)

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

func TestUnknownRegistryKindsAreStableBoundedAndIsolated(t *testing.T) {
	first := resourceKindForRegistry("ExampleMod:Widgets")
	normalized := resourceKindForRegistry("  examplemod:widgets  ")
	second := resourceKindForRegistry("OtherMod:Widgets")
	if first != normalized {
		t.Fatalf("normalized registry identity changed: %q != %q", first, normalized)
	}
	if first == second {
		t.Fatalf("different unknown registries collapsed into %q", first)
	}
	if !strings.HasPrefix(first, "import.document.") || len(first) != len("import.document.")+24 {
		t.Fatalf("unknown registry kind is not a bounded digest: %q", first)
	}
	malicious := resourceKindForRegistry(strings.Repeat("../VERY-LONG/", 1_000))
	if len(malicious) != len("import.document.")+24 || strings.ContainsAny(malicious, "/\\:") {
		t.Fatalf("attacker-controlled registry escaped bounded kind encoding: %q", malicious)
	}
	resourceID := "example:shared"
	if resourceIdentity(first, resourceID).ID == resourceIdentity(second, resourceID).ID {
		t.Fatal("unknown registries with the same object ID shared a resource identity")
	}
}
