package httpapi

import (
	"strings"
	"testing"
)

func TestDimensionDocumentMergesDimensionTypeAndDirectBiome(t *testing.T) {
	document := map[string]any{
		"dimensions": []any{map[string]any{
			"id": "minecraft:test", "namespace": "minecraft",
			"runtime_definition": map[string]any{
				"type":      "minecraft:test_type",
				"generator": map[string]any{"settings": map[string]any{"biome": "minecraft:plains"}},
			},
		}},
		"dimension_types": []any{map[string]any{
			"id":         "minecraft:test_type",
			"definition": map[string]any{"height": float64(384), "natural": true},
		}},
	}
	entries := exportDocumentEntries("worldgen/dimensions.json", document)
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	dimensionType := exportObject(entries[0].Data["dimension_type_definition"])
	if dimensionType["height"] != float64(384) || dimensionType["natural"] != true {
		t.Fatalf("dimension type = %#v", dimensionType)
	}
	biomes := entries[0].Data["biome_ids"].([]any)
	if len(biomes) != 1 || biomes[0] != "minecraft:plains" {
		t.Fatalf("biomes = %#v", biomes)
	}
}

func TestBiomeDocumentExtractsReferenceLists(t *testing.T) {
	document := map[string]any{"biomes": []any{map[string]any{
		"id": "minecraft:test", "namespace": "minecraft",
		"runtime_definition": map[string]any{
			"spawners": map[string]any{"creature": []any{map[string]any{"type": "minecraft:cow"}}},
			"features": []any{[]any{"minecraft:trees_plains"}},
			"carvers":  map[string]any{"air": []any{"minecraft:cave"}},
		},
	}}}
	entry := exportDocumentEntries("worldgen/biomes.json", document)[0]
	if got := entry.Data["spawned_entity_ids"].([]any); len(got) != 1 || got[0] != "minecraft:cow" {
		t.Fatalf("spawned entities = %#v", got)
	}
	if got := entry.Data["feature_ids"].([]string); len(got) != 1 || got[0] != "minecraft:trees_plains" {
		t.Fatalf("features = %#v", got)
	}
	if got := entry.Data["carver_ids"].([]string); len(got) != 1 || got[0] != "minecraft:cave" {
		t.Fatalf("carvers = %#v", got)
	}
}

func TestStructureDocumentSplitsBiomeTagsAndIDs(t *testing.T) {
	tag, ids := exportBiomeSelectorReferences("#minecraft:has_structure/test")
	if tag != "#minecraft:has_structure/test" || len(ids) != 0 {
		t.Fatalf("tag=%q ids=%#v", tag, ids)
	}
	tag, ids = exportBiomeSelectorReferences([]any{"minecraft:plains", "minecraft:forest"})
	if tag != "" || len(ids) != 2 {
		t.Fatalf("tag=%q ids=%#v", tag, ids)
	}
}

func TestExportRevisionRequiresExactNamespace(t *testing.T) {
	revisions := map[string]string{"example": "revision-example"}
	revisionID, err := exportRevisionForNamespace(revisions, " Example ")
	if err != nil || revisionID != "revision-example" {
		t.Fatalf("exact namespace did not resolve: revision=%q err=%v", revisionID, err)
	}
	if _, err = exportRevisionForNamespace(revisions, "missing"); err == nil {
		t.Fatal("single-revision package silently accepted an unknown namespace")
	}
	if _, err = exportRevisionForNamespace(revisions, ""); err == nil {
		t.Fatal("empty namespace was accepted")
	}
	if _, err = exportRevisionForRecipeType(revisions, "missing:machine"); err == nil {
		t.Fatal("recipe type from an unknown namespace was assigned to the only revision")
	}
}

func TestImportQueuesRejectUnknownNamespacesInsteadOfDroppingEntries(t *testing.T) {
	revisions := map[string]string{"example": "revision-example"}
	document := []byte(`{"biomes":[{"id":"missing:test","namespace":"missing","names":{"en-US":"Missing"}}]}`)
	err := queueExportDocumentEntries(newModExportWriteBatch(), nil, revisions, "worldgen/biomes.json", document)
	if err == nil || !strings.Contains(err.Error(), `namespace "missing" has no import revision`) {
		t.Fatalf("document entry was not rejected with structured namespace context: %v", err)
	}
	err = queueExportJEITemplateCollection(newModExportWriteBatch(), revisions, "recipes/jei/templates/missing.json", exportJEITemplateCollection{
		RecipeTypeID: "missing:machine",
	})
	if err == nil || !strings.Contains(err.Error(), `recipe type "missing:machine"`) {
		t.Fatalf("recipe template was not rejected with recipe namespace context: %v", err)
	}
}
