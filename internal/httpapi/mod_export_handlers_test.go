package httpapi

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestQueueExportRecipeLayoutCollection(t *testing.T) {
	batch := newModExportWriteBatch()
	raw := []byte(`{
		"schema_version":"mcmods-jei-layout-collection/v1",
		"recipe_type_id":"minecraft:crafting",
		"layouts":[
			{"schema_version":"mcmods-jei-layout/v4","recipe_id":"minecraft:test","recipe_id_source":"minecraft_recipe","recipe_id_canonical":true,
			 "layout_key":"minecraft__test","slots":[{"role":"input","item_tag_equivalent":"minecraft:logs","alternatives":[{"item":"minecraft:oak_log"},{"item":"minecraft:birch_log"}]}]},
			{"schema_version":"mcmods-jei-layout/v4","recipe_id":"minecraft:crafting/__generated/recipe_000001","recipe_id_source":"generated_index","recipe_id_canonical":false,
			 "layout_key":"generated_1","slots":[{"role":"output","alternatives":[{"item":"minecraft:stick","count":4}]}]}
		]
	}`)
	if err := queueExportRecipeLayout(batch, "package", "revision", "recipes/jei/layouts/minecraft/crafting.json", raw); err != nil {
		t.Fatal(err)
	}
	if batch.rows < 20 {
		t.Fatalf("expected catalog entities, snapshots and ingredient references to be queued, got %d rows", batch.rows)
	}
	if tagID := exportRecipeSlotTagID(map[string]any{"item_tag_equivalent": " minecraft:logs "}); tagID != "minecraft:logs" {
		t.Fatalf("unexpected precomputed tag ID %q", tagID)
	}
}

func TestQueueExportRecipeLayoutRejectsIncompleteIdentity(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "missing recipe id",
			raw:  `{"schema_version":"mcmods-jei-layout/v4","recipe_type_id":"minecraft:crafting","recipe_id_source":"generated_index","recipe_id_canonical":false}`,
		},
		{
			name: "missing identity source",
			raw:  `{"schema_version":"mcmods-jei-layout/v4","recipe_type_id":"minecraft:crafting","recipe_id":"minecraft:test","recipe_id_canonical":true}`,
		},
		{
			name: "unsupported identity source",
			raw:  `{"schema_version":"mcmods-jei-layout/v4","recipe_type_id":"minecraft:crafting","recipe_id":"minecraft:test","recipe_id_source":"legacy","recipe_id_canonical":false}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			batch := newModExportWriteBatch()
			if err := queueExportRecipeLayout(batch, "package", "revision", "layout.json", []byte(test.raw)); err == nil {
				t.Fatal("expected incomplete recipe identity to be rejected")
			}
		})
	}
}

func TestValidateModExportManifestRequiresZipPackage(t *testing.T) {
	manifest := modExportManifest{SchemaVersion: modExportSchemaVersion, ExporterVersion: "0.7.0", Status: "complete", MinecraftVersion: "1.20.1", Loader: "forge", PackageFormat: "directory"}
	manifest.Configuration.Namespaces = []string{"minecraft"}
	if err := validateModExportManifest(manifest); err == nil {
		t.Fatal("expected non-ZIP package to be rejected")
	}
	manifest.PackageFormat = "zip"
	if err := validateModExportManifest(manifest); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeModExportCapabilities(t *testing.T) {
	manifest := modExportManifest{SchemaVersion: modExportSchemaVersion, MinecraftVersion: "1.20.1", Loader: "forge"}
	capabilities, err := decodeModExportCapabilities([]byte(`{
		"schema_version":"mcmods-capabilities/v1","minecraft_version":"1.20.1","loader":"forge",
		"format_contract":"mcmods-export/v1","capabilities":[
			{"id":"registries","status":"available","source":"built_in_registry"},
			{"id":"jei_layouts","status":"degraded","source":"required_jei_runtime"}
		]
	}`), manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(capabilities) != 2 || capabilities[1].Status != "degraded" {
		t.Fatalf("unexpected capabilities: %#v", capabilities)
	}
}

func TestExportIngredientKindKeepsMekanismFamiliesSeparate(t *testing.T) {
	tests := map[string]string{
		"item_stack":                                 "item",
		"fluid_stack":                                "fluid",
		"mekanism.api.chemical.gas.GasStack":         "mekanism_gas",
		"mekanism.api.chemical.infuse.InfusionStack": "mekanism_infuse_type",
		"mekanism.api.chemical.pigment.PigmentStack": "mekanism_pigment",
		"mekanism.api.chemical.slurry.SlurryStack":   "mekanism_slurry",
	}
	for input, expected := range tests {
		if actual := exportIngredientKind(input); actual != expected {
			t.Fatalf("ingredient type %q mapped to %q, expected %q", input, actual, expected)
		}
	}
}

func TestExportDocumentEntriesPrecomputesNestedIndexes(t *testing.T) {
	ingredients := exportDocumentEntries("ingredients/ingredients.json", map[string]any{
		"types": []any{map[string]any{
			"ingredient_type": "minecraft:item_stack",
			"entries": []any{map[string]any{
				"resource_location": "minecraft:oak_log",
				"names":             map[string]any{"en_us": "Oak Log"},
				"icons":             map[string]any{"32": "icons/items/32/minecraft/oak_log.png"},
			}},
		}},
	})
	if len(ingredients) != 1 || ingredients[0].ID != "minecraft:oak_log" ||
		ingredients[0].Namespace != "minecraft" || ingredients[0].IconPath == "" ||
		ingredients[0].Data["ingredient_type"] != "minecraft:item_stack" {
		t.Fatalf("unexpected ingredient projection: %#v", ingredients)
	}

	naturalGeneration := exportDocumentEntries("worldgen/natural_generation.json", map[string]any{
		"categories": []any{map[string]any{
			"category": "configured_feature", "registry": "minecraft:worldgen/configured_feature",
			"entries": []any{map[string]any{"id": "minecraft:ore_coal"}},
		}},
	})
	if len(naturalGeneration) != 1 || naturalGeneration[0].Data["category"] != "configured_feature" {
		t.Fatalf("unexpected natural generation projection: %#v", naturalGeneration)
	}
}

func TestNormalizeExportPathRejectsUnsafePaths(t *testing.T) {
	for _, value := range []string{"../manifest.json", "/manifest.json", `C:\\manifest.json`, `folder\\manifest.json`, ""} {
		if _, err := normalizeExportPath(value); err == nil {
			t.Fatalf("expected unsafe path %q to fail", value)
		}
	}
	if normalized, err := normalizeExportPath("data/example/structures/test.nbt"); err != nil || normalized != "data/example/structures/test.nbt" {
		t.Fatalf("unexpected normalized path %q: %v", normalized, err)
	}
}

func TestValidateExportZIPRejectsCaseInsensitiveDuplicates(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range []string{"manifest.json", "MANIFEST.JSON"} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write([]byte("{}"))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = validateExportZIP(reader.File); err == nil {
		t.Fatal("expected duplicate normalized ZIP paths to fail")
	}
}

func TestDeriveModExportBlockBindingsUsesRegistryRelationships(t *testing.T) {
	files := testExportZIPFiles(t, map[string]string{
		"registries/blocks.json":                      `{"registry":"blocks","entries":[{"id":"example:machine","namespace":"example","path":"machine","item":"example:machine"}]}`,
		"registries/items.json":                       `{"registry":"items","entries":[{"id":"example:machine","namespace":"example","path":"machine","block":"example:machine"},{"id":"example:wrench","namespace":"example","path":"wrench"}]}`,
		"assets/example/blockstates/machine.json":     `{"variants":{"":{"model":"example:block/machine/on"}}}`,
		"assets/example/models/block/machine/on.json": `{"parent":"minecraft:block/cube_all","textures":{"all":"example:block/machine"}}`,
		"assets/example/models/item/machine.json":     `{"parent":"example:block/machine/on"}`,
		"assets/example/models/item/wrench.json":      `{"parent":"minecraft:item/generated","textures":{"layer0":"example:item/wrench"}}`,
		"assets/minecraft/models/block/cube_all.json": `{"textures":{"particle":"#all"}}`,
		"assets/example/textures/block/machine.png":   "png",
		"assets/example/textures/item/wrench.png":     "png",
	})
	bindings, err := deriveModExportBlockBindings(files, map[string]string{"example": "revision-example"})
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 {
		t.Fatalf("expected one block binding, got %#v", bindings)
	}
	binding := bindings[0]
	if binding.BlockID != "example:machine" || binding.ItemID != "example:machine" {
		t.Fatalf("unexpected registry relationship: %#v", binding)
	}
	if binding.Blockstate != "assets/example/blockstates/machine.json" || binding.ItemModel != "assets/example/models/item/machine.json" {
		t.Fatalf("unexpected root assets: %#v", binding)
	}
	for _, expected := range []string{
		"assets/example/models/block/machine/on.json",
		"assets/example/models/item/machine.json",
		"assets/minecraft/models/block/cube_all.json",
	} {
		if !containsString(binding.ModelPaths, expected) {
			t.Fatalf("missing model %s in %#v", expected, binding.ModelPaths)
		}
	}
	if !containsString(binding.TexturePaths, "assets/example/textures/block/machine.png") {
		t.Fatalf("missing block texture in %#v", binding.TexturePaths)
	}
}

func testExportZIPFiles(t *testing.T, values map[string]string) map[string]*zip.File {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, value := range values {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files, err := validateExportZIP(reader.File)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestNormalizeExportNamespaces(t *testing.T) {
	actual := normalizeExportNamespaces([]string{" Create ", "create", "example-mod", "../bad"})
	if len(actual) != 2 || actual[0] != "create" || actual[1] != "example-mod" {
		t.Fatalf("unexpected namespaces: %#v", actual)
	}
}

func TestDecodeExportTranslationValuesSkipsNonStrings(t *testing.T) {
	values, skippedKeys, total, err := decodeExportTranslationValues([]byte(`{"plain":"value","nested":{"text":"value"},"count":3,"empty":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(values) != 1 || values["plain"] != "value" {
		t.Fatalf("unexpected decoded translations: total=%d values=%#v", total, values)
	}
	if len(skippedKeys) != 3 {
		t.Fatalf("expected three skipped keys, got %#v", skippedKeys)
	}
}

func TestDecodeExportTagsAssociatesSharedTagsWithRelevantRevisions(t *testing.T) {
	revisions := map[string]string{"minecraft": "revision-minecraft", "create": "revision-create"}
	raw := []byte(`{
		"schema_version":"mcmods-tags/v1",
		"registries":[{
			"registry":"minecraft:item",
			"tags":[
				{"id":"forge:ingots/zinc","values":["create:zinc_ingot","create:zinc_ingot"]},
				{"id":"minecraft:logs","values":["minecraft:oak_log","create:weathered_limestone"]}
			]
		}]
	}`)
	rows, memberCount, err := decodeExportTags(revisions, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 revision-tag rows, got %d: %#v", len(rows), rows)
	}
	if memberCount != 5 {
		t.Fatalf("expected 5 member rows, got %d", memberCount)
	}
	for _, row := range rows {
		if row.TagID == "forge:ingots/zinc" && (row.RevisionID != "revision-create" || len(row.Members) != 1) {
			t.Fatalf("unexpected shared Forge tag row: %#v", row)
		}
		if row.TagID == "minecraft:logs" && len(row.Members) != 2 {
			t.Fatalf("tag members must remain complete for every relevant revision: %#v", row)
		}
	}
}

func TestDecodeExportTranslationValuesSupportsExporterWrapper(t *testing.T) {
	values, skippedKeys, total, err := decodeExportTranslationValues([]byte(`{
		"language":"zh_cn",
		"namespace":"minecraft",
		"translation_count":3,
		"translations":{"block.minecraft.stone":"石头","item.minecraft.apple":"苹果","invalid":{"text":"bad"}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(values) != 2 || values["block.minecraft.stone"] != "石头" || values["item.minecraft.apple"] != "苹果" {
		t.Fatalf("unexpected wrapped translations: total=%d values=%#v", total, values)
	}
	if len(skippedKeys) != 1 || skippedKeys[0] != "invalid" {
		t.Fatalf("unexpected skipped keys: %#v", skippedKeys)
	}
}

func TestShouldRetryModExportStatus(t *testing.T) {
	for _, status := range []string{"failed", "cancelled"} {
		if !shouldRetryModExportStatus(status) {
			t.Fatalf("expected %s to be retryable", status)
		}
	}
	for _, status := range []string{"queued", "validating", "importing", "ready", "partial"} {
		if shouldRetryModExportStatus(status) {
			t.Fatalf("expected %s not to be retryable", status)
		}
	}
}

func TestRecipeIdentityScope(t *testing.T) {
	canonicalA := recipeIdentity("package-a", "minecraft:crafting", "example:recipe", true)
	canonicalB := recipeIdentity("package-b", "minecraft:crafting", "example:recipe", true)
	if canonicalA != canonicalB {
		t.Fatalf("authoritative recipe identity changed across packages: %#v != %#v", canonicalA, canonicalB)
	}
	generatedA := recipeIdentity("package-a", "example:machine", "example:machine/__generated/recipe_000001", false)
	generatedB := recipeIdentity("package-b", "example:machine", "example:machine/__generated/recipe_000001", false)
	if generatedA == generatedB {
		t.Fatalf("generated recipe identity must remain package scoped: %#v", generatedA)
	}
}
