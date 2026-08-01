package httpapi

import "testing"

func TestCanonicalModContentDefinitionKeepsConfiguredFieldsOnly(t *testing.T) {
	entryType := modContentEntryTypeDefinition{Code: "entity", Groups: []modContentEntryTypeGroup{{
		Code: "entity",
		Fields: []modContentEntryTypeField{
			{Code: "maxHealth", Type: "number", Paths: [][]string{{"max_health"}}},
			{Code: "defaultEquipment", Type: "json", Paths: [][]string{{"default_equipment"}}},
		},
	}}}
	definition, err := canonicalModContentDefinition(entryType, map[string]any{
		"max_health":            float64(20),
		"default_equipment":     map[string]any{"mainhand": "minecraft:iron_sword"},
		"registered_attributes": map[string]any{"generic.max_health": float64(20)},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if definition["maxHealth"] != float64(20) {
		t.Fatalf("maxHealth = %#v", definition["maxHealth"])
	}
	if _, exists := definition["registered_attributes"]; exists {
		t.Fatal("unconfigured exporter field was retained")
	}
	if _, exists := definition["max_health"]; exists {
		t.Fatal("exporter alias was used as a storage key")
	}
}

func TestCanonicalModContentDefinitionManualWritesUseFieldCodes(t *testing.T) {
	entryType := modContentEntryTypeDefinition{Code: "block", Groups: []modContentEntryTypeGroup{{
		Code:   "physical",
		Fields: []modContentEntryTypeField{{Code: "explosionResistance", Type: "number", Paths: [][]string{{"explosion_resistance"}}}},
	}}}
	definition, err := canonicalModContentDefinition(entryType, map[string]any{
		"explosionResistance":  float64(6),
		"explosion_resistance": float64(99),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if definition["explosionResistance"] != float64(6) || len(definition) != 1 {
		t.Fatalf("definition = %#v", definition)
	}
}

func TestCanonicalModContentDefinitionImportAliasesTakePrecedence(t *testing.T) {
	entryType := modContentEntryTypeDefinition{Code: "tool", Groups: []modContentEntryTypeGroup{{
		Code: "tool",
		Fields: []modContentEntryTypeField{{
			Code: "durability", Type: "number", Paths: [][]string{{"max_damage"}, {"durability", "max_damage"}},
		}},
	}}}
	definition, err := canonicalModContentDefinition(entryType, map[string]any{
		"max_damage": float64(384),
		"durability": map[string]any{"damageable": true, "max_damage": float64(384)},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if definition["durability"] != float64(384) {
		t.Fatalf("durability = %#v", definition["durability"])
	}
}

func TestCanonicalResourceDefinitionDowngradesInvalidInferredToolToItem(t *testing.T) {
	template := modContentTemplateDefinition{EntryTypes: []modContentEntryTypeDefinition{
		{Code: "item", KindCodes: []string{"minecraft.item"}, Groups: []modContentEntryTypeGroup{{
			Code: "item", Fields: []modContentEntryTypeField{{Code: "maxStackSize", Type: "number", Paths: [][]string{{"max_stack_size"}}}},
		}}},
		{Code: "tool", KindCodes: []string{"minecraft.item"}, Groups: []modContentEntryTypeGroup{{
			Code: "tool", Fields: []modContentEntryTypeField{{Code: "durability", Type: "number", Paths: [][]string{{"max_damage"}}}},
		}}},
	}}
	entryTypeCode, definition, err := canonicalResourceDefinitionFromTemplates(
		map[string]modContentTemplateDefinition{"minecraft.item": template},
		"minecraft.item",
		"custom_tool",
		map[string]any{
			"max_stack_size": float64(1),
			"durability":     map[string]any{"damageable": true},
			"tool":           map[string]any{"types": []any{"custom_tool"}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if entryTypeCode != "item" {
		t.Fatalf("entry type = %q", entryTypeCode)
	}
	if definition["maxStackSize"] != float64(1) {
		t.Fatalf("definition = %#v", definition)
	}
}

func TestCanonicalNaturalGenerationSizePreservesNumberProvider(t *testing.T) {
	entryType := modContentEntryTypeDefinition{Code: "natural_generation", Groups: []modContentEntryTypeGroup{{
		Code: "website_data",
		Fields: []modContentEntryTypeField{{
			Code: "size", Type: "json", Paths: [][]string{{"size"}},
		}},
	}}}
	definition, err := canonicalModContentDefinition(entryType, map[string]any{
		"size": map[string]any{
			"min_inclusive": float64(3),
			"max_inclusive": float64(7),
			"type":          "minecraft:uniform",
		},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	size, ok := definition["size"].(map[string]any)
	if !ok || size["type"] != "minecraft:uniform" || size["min_inclusive"] != float64(3) || size["max_inclusive"] != float64(7) {
		t.Fatalf("size = %#v", definition["size"])
	}
}

func TestCanonicalWorldStructureBiomesPreservesTagSelector(t *testing.T) {
	entryType := modContentEntryTypeDefinition{Code: "world_structure", Groups: []modContentEntryTypeGroup{{
		Code: "website_data",
		Fields: []modContentEntryTypeField{{
			Code: "biomes", Type: "json", Paths: [][]string{{"biomes"}},
		}},
	}}}
	definition, err := canonicalModContentDefinition(entryType, map[string]any{
		"biomes": "#minecraft:has_structure/ancient_city",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if definition["biomes"] != "#minecraft:has_structure/ancient_city" {
		t.Fatalf("biomes = %#v", definition["biomes"])
	}
}

func TestInferImportedEntryTypeCode(t *testing.T) {
	if value := inferImportedEntryTypeCode("minecraft.item", "diamond_chestplate", map[string]any{}); value != "equipment" {
		t.Fatalf("equipment inference = %q", value)
	}
	if value := inferImportedEntryTypeCode("minecraft.item", "wrench", map[string]any{"max_damage": float64(300)}); value != "tool" {
		t.Fatalf("tool inference = %q", value)
	}
	if value := inferImportedEntryTypeCode("minecraft.entity_type", "cow", map[string]any{}); value != "entity" {
		t.Fatalf("entity inference = %q", value)
	}
}
