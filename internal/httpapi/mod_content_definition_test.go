package httpapi

import (
	"reflect"
	"testing"
)

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
			Code: "biomeTag", Type: "reference", ReferenceKind: "tag", Paths: [][]string{{"biome_tag"}},
		}},
	}}}
	definition, err := canonicalModContentDefinition(entryType, map[string]any{
		"biome_tag": "#minecraft:has_structure/ancient_city",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if definition["biomeTag"] != "#minecraft:has_structure/ancient_city" {
		t.Fatalf("biomeTag = %#v", definition["biomeTag"])
	}
}

func TestManualEntryTypeSelectionIsIndependentFromResourceKind(t *testing.T) {
	entryTypes := []modContentEntryTypeDefinition{
		{Code: "block", KindCodes: []string{"minecraft.block"}},
		{Code: "tool", KindCodes: []string{"minecraft.item"}},
	}
	if _, err := selectModContentEntryType(entryTypes, "tool", "minecraft.block", false); err != nil {
		t.Fatalf("manual category selection was coupled to resource kind: %v", err)
	}
	if _, err := selectModContentEntryType(entryTypes, "tool", "minecraft.block", true); err == nil {
		t.Fatal("import category validation must still enforce its inferred resource kind")
	}
}

func TestImportedDimensionBiomeRelationsAreBidirectional(t *testing.T) {
	rows := []catalogResourceImportRow{{
		KindCode: "minecraft.natural_generation",
		Data:     `{"dimension_ids":["minecraft:overworld"],"resolved_biome_ids":["minecraft:plains","minecraft:forest"]}`,
	}}
	dimensions, biomes := importedDimensionBiomeRelations(rows)
	if len(dimensions["minecraft:overworld"]) != 2 {
		t.Fatalf("dimension biomes = %#v", dimensions)
	}
	if got := biomes["minecraft:forest"]; len(got) != 1 || got[0] != "minecraft:overworld" {
		t.Fatalf("biome dimensions = %#v", biomes)
	}
}

func TestImportedAdvancementLayoutGroupsFollowConnectedComponents(t *testing.T) {
	rows := []catalogResourceImportRow{
		{KindCode: "minecraft.advancement", CanonicalID: "example:root", Data: `{}`},
		{KindCode: "minecraft.advancement", CanonicalID: "example:child", Data: `{"parent":"example:root"}`},
		{KindCode: "minecraft.advancement", CanonicalID: "example:standalone", Data: `{}`},
	}
	groups := importedAdvancementLayoutGroups(rows)
	if groups["example:root"] == "" || groups["example:root"] != groups["example:child"] {
		t.Fatalf("connected advancements were split: %#v", groups)
	}
	if groups["example:standalone"] == groups["example:root"] {
		t.Fatalf("unconnected advancement joined another group: %#v", groups)
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
	for kindCode, expected := range map[string]string{
		"minecraft.dimension":   "dimension",
		"minecraft.biome":       "biome",
		"minecraft.key_mapping": "key_mapping",
	} {
		if value := inferImportedEntryTypeCode(kindCode, "example", map[string]any{}); value != expected {
			t.Errorf("%s inference = %q, want %q", kindCode, value, expected)
		}
	}
}

func TestMergeModContentDefinitionPatch(t *testing.T) {
	base := map[string]any{
		"unchanged": "keep",
		"removed":   "drop",
		"nested": map[string]any{
			"unchanged": float64(1),
			"changed":   false,
		},
	}
	patch := map[string]any{
		"removed": nil,
		"nested": map[string]any{
			"changed": true,
		},
	}

	got := mergeModContentDefinitionPatch(base, patch)
	want := map[string]any{
		"unchanged": "keep",
		"nested": map[string]any{
			"unchanged": float64(1),
			"changed":   true,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged definition = %#v, want %#v", got, want)
	}
	if base["removed"] != "drop" {
		t.Fatal("merge mutated its base document")
	}
}

func TestNormalizeLootTableCanonicalDefinitionRefreshesDerivedReferences(t *testing.T) {
	definition := map[string]any{
		"pools": []any{map[string]any{"entries": []any{
			map[string]any{"type": "minecraft:item", "name": "minecraft:diamond"},
			map[string]any{"type": "minecraft:loot_table", "value": "minecraft:chests/abandoned_mineshaft"},
			map[string]any{"type": "minecraft:alternatives", "children": []any{
				map[string]any{"type": "minecraft:item", "name": "minecraft:emerald"},
			}},
		}}},
		"possibleItemIds":      []any{"minecraft:stale"},
		"referencedLootTables": []any{"minecraft:stale"},
	}

	normalizeLootTableCanonicalDefinition(definition)
	if got, want := definition["possibleItemIds"], []string{"minecraft:diamond", "minecraft:emerald"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("possible items = %#v, want %#v", got, want)
	}
	if got, want := definition["referencedLootTables"], []string{"minecraft:chests/abandoned_mineshaft"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("referenced loot tables = %#v, want %#v", got, want)
	}
	if definition["definitionAvailable"] != true {
		t.Fatal("loot-table definition must be marked available when pools are present")
	}
}

func TestCanonicalAdvancementDefinitionPreservesGraphAndProgressData(t *testing.T) {
	entryType := modContentEntryTypeDefinition{Code: "advancement", Groups: []modContentEntryTypeGroup{{
		Code: "website_data",
		Fields: []modContentEntryTypeField{
			{Code: "parentId", Type: "reference", Paths: [][]string{{"parent"}}},
			{Code: "childrenIds", Type: "reference-list", Paths: [][]string{{"children"}}},
			{Code: "criteria", Type: "list", Paths: [][]string{{"criteria"}}},
			{Code: "requirements", Type: "json", Paths: [][]string{{"requirements"}}},
			{Code: "rewards", Type: "json", Paths: [][]string{{"rewards"}}},
		},
	}}}
	definition, err := canonicalModContentDefinition(entryType, map[string]any{
		"parent":       "minecraft:story/root",
		"children":     []any{"minecraft:story/child"},
		"criteria":     []any{"entered_world"},
		"requirements": []any{[]any{"entered_world"}},
		"rewards":      map[string]any{"experience": float64(100)},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if definition["parentId"] != "minecraft:story/root" {
		t.Fatalf("parentId = %#v", definition["parentId"])
	}
	if got := definition["childrenIds"].([]string); len(got) != 1 || got[0] != "minecraft:story/child" {
		t.Fatalf("childrenIds = %#v", got)
	}
	if _, exists := definition["requirements"]; !exists {
		t.Fatal("advancement requirements were discarded")
	}
	if _, exists := definition["rewards"]; !exists {
		t.Fatal("advancement rewards were discarded")
	}
}

func TestCanonicalModContentDefinitionValidatesDisplayFormats(t *testing.T) {
	entryType := modContentEntryTypeDefinition{Code: "entity", Groups: []modContentEntryTypeGroup{{
		Code: "entity",
		Fields: []modContentEntryTypeField{
			{Code: "maxHealth", Type: "number", Format: "health", Paths: [][]string{{"max_health"}}},
			{Code: "spawnRange", Type: "range", Format: "range", Paths: [][]string{{"spawn_range"}}},
		},
	}}}
	definition, err := canonicalModContentDefinition(entryType, map[string]any{
		"max_health":  float64(20),
		"spawn_range": []any{float64(4), float64(12)},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := definition["spawnRange"]; !reflect.DeepEqual(got, []float64{4, 12}) {
		t.Fatalf("spawnRange = %#v", got)
	}
	if _, err = canonicalModContentDefinition(entryType, map[string]any{"max_health": 20.5}, true); err == nil {
		t.Fatal("fractional health value was accepted")
	}
	if _, err = canonicalModContentDefinition(entryType, map[string]any{"spawn_range": []any{12.0, 4.0}}, true); err == nil {
		t.Fatal("descending range was accepted")
	}
}

func TestConfiguredEntryTypeMatchingUsesDistinctiveImportFields(t *testing.T) {
	template := modContentTemplateDefinition{ResourceKinds: []string{"minecraft.item"}, EntryTypes: []modContentEntryTypeDefinition{
		{Code: "item", KindCodes: []string{"minecraft.item"}, Groups: []modContentEntryTypeGroup{{Code: "item", Fields: []modContentEntryTypeField{
			{Code: "maxStackSize", Type: "number", Paths: [][]string{{"max_stack_size"}}},
		}}}},
		{Code: "machine", KindCodes: []string{"minecraft.item"}, Groups: []modContentEntryTypeGroup{{Code: "machine", Fields: []modContentEntryTypeField{
			{Code: "maxEnergy", Type: "number", Paths: [][]string{{"machine", "max_energy"}}},
		}}}},
	}}
	got := matchImportedEntryType(template, "minecraft.item", map[string]any{
		"max_stack_size": float64(1),
		"machine":        map[string]any{"max_energy": float64(10000)},
	}, "item")
	if got != "machine" {
		t.Fatalf("matched entry type = %q, want machine", got)
	}
}

func TestResourceAttributeSchemaPreservesStableIDsAndStorageTypes(t *testing.T) {
	current := modContentTemplateDefinition{EntryTypes: []modContentEntryTypeDefinition{{
		Code: "entity", KindCodes: []string{"minecraft.entity_type"}, Groups: []modContentEntryTypeGroup{{Code: "entity", Fields: []modContentEntryTypeField{{
			Code: "maxHealth", Type: "number", Format: "float",
		}}}},
	}}}
	next := current
	next.EntryTypes = append([]modContentEntryTypeDefinition(nil), current.EntryTypes...)
	next.EntryTypes[0].Groups = append([]modContentEntryTypeGroup(nil), current.EntryTypes[0].Groups...)
	next.EntryTypes[0].Groups[0].Fields = append([]modContentEntryTypeField(nil), current.EntryTypes[0].Groups[0].Fields...)
	next.EntryTypes[0].Groups[0].Fields[0].Format = "health"
	if err := preservesModContentAttributeSchema(current, next); err != nil {
		t.Fatalf("presentation-only numeric format change was rejected: %v", err)
	}
	next.EntryTypes[0].Groups[0].Fields[0].Type = "text"
	if err := preservesModContentAttributeSchema(current, next); err == nil {
		t.Fatal("storage type change was accepted")
	}
}
