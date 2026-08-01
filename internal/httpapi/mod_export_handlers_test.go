package httpapi

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestQueueExportRecipeTemplateAndBindings(t *testing.T) {
	batch := newModExportWriteBatch()
	templateRaw := []byte(`{
		"schema_version":"mcmods-jei-template-collection/v2",
		"recipe_type_id":"minecraft:crafting",
		"coordinate_space":"logical_pixels","image_scale":4,
		"canvas":{"width":178,"height":86},"image_pixels":{"width":712,"height":344},
		"template_count":1,"templates":[{"schema_version":"mcmods-jei-layout-template/v2",
		"template_id":"template_test","background":"recipes/jei/backgrounds/minecraft/crafting/template_test.png",
		"slot_count":2,"slots":[
			{"slot_id":"input_0","role":"input","jei_role":"input","coordinates_available":true,"rect":{"x":30,"y":34,"width":18,"height":18}},
			{"slot_id":"output_0","role":"output","jei_role":"output","output_index":0,"coordinates_available":true,"rect":{"x":130,"y":34,"width":18,"height":18}}
		]}]
	}`)
	templateDocument, err := decodeExportJEITemplateCollection(templateRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err = queueExportJEITemplateCollection(batch, map[string]string{"minecraft": "revision"}, "recipes/jei/templates/minecraft/crafting.json", templateDocument); err != nil {
		t.Fatal(err)
	}
	recipeRaw := []byte(`{"schema_version":"mcmods-jei-recipe-collection/v2","recipe_type_id":"minecraft:crafting",
		"template_collection":"recipes/jei/templates/minecraft/crafting.json","count":1,"recipes":[{
		"schema_version":"mcmods-jei-recipe-bindings/v2","recipe_type_id":"minecraft:crafting",
		"recipe_id":"minecraft:test","recipe_id_source":"minecraft_recipe","recipe_id_canonical":true,
		"origin_kind":"registered_recipe","underlying_recipe_type_id":"minecraft:crafting",
		"source_mod_id":"minecraft","source_mod_version":"1.20.1","source_mod_id_source":"recipe_id_namespace",
		"recipe_key":"minecraft:test#000000","template_id":"template_test","layout_kind":"shaped","ordered":true,
		"layout_classification_source":"recipe_class","width":1,"height":1,"binding_count":2,"bindings":[
			{"slot_id":"input_0","ingredient_present":true,"clickable":true,"item_tag_equivalent":"minecraft:logs","alternatives":[{"type":"minecraft:item_stack","item":"minecraft:oak_log","count":1}]},
			{"slot_id":"output_0","semantic_role":"byproduct","role_source":"jei_slot_tooltip","ingredient_present":true,"clickable":true,
			"chance_available":true,"chance":0.4,"chance_percent":40,"chance_comparator":"exact","chance_source":"jei_slot_tooltip",
			"chance_text":"40% Chance","chance_texts":{"zh_cn":"40% 概率"},"chance_translation_key":"recipe.chance","byproduct":true,
			"alternatives":[{"type":"minecraft:item_stack","item":"minecraft:stick","count":1}]}
		]}]}`)
	recipeDocument, err := decodeExportJEIRecipeCollection(recipeRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err = queueExportJEIRecipeCollection(batch, catalogResourceIdentityResolver{}, "package", map[string]string{"minecraft": "revision"}, "recipes/jei/recipes/minecraft/crafting.json", recipeDocument); err != nil {
		t.Fatal(err)
	}
	if batch.rows != 9 || len(batch.recipes) != 1 || len(batch.recipeBindings) != 2 || len(batch.recipeCandidates) != 2 {
		t.Fatalf(
			"expected template writes plus 1 recipe, 2 bindings and 2 candidates; got queued=%d recipes=%d bindings=%d candidates=%d",
			batch.rows, len(batch.recipes), len(batch.recipeBindings), len(batch.recipeCandidates),
		)
	}
}

func TestCompactRecipeImportJSONRemovesDuplicatedChildren(t *testing.T) {
	compacted := compactImportJSONObject(
		[]byte(`{"recipe_id":"minecraft:test","bindings":[{"slot_id":"input"}],"metadata":{"source":"test"}}`),
		"bindings",
	)
	if bytes.Contains(compacted, []byte(`"bindings"`)) {
		t.Fatalf("nested bindings were retained: %s", compacted)
	}
	if !bytes.Contains(compacted, []byte(`"recipe_id":"minecraft:test"`)) ||
		!bytes.Contains(compacted, []byte(`"metadata":{"source":"test"}`)) {
		t.Fatalf("recipe metadata was lost: %s", compacted)
	}
}

func TestCompactRegistryImportJSONRemovesNormalizedFields(t *testing.T) {
	compacted := compactImportJSONObject(
		[]byte(`{"id":"minecraft:stone","namespace":"minecraft","path":"stone","translation_key":"block.minecraft.stone","names":{"zh-CN":"石头"},"hardness":1.5,"technical":{"solid":true}}`),
		"id", "namespace", "path", "translation_key", "names",
	)
	for _, field := range [][]byte{
		[]byte(`"id"`),
		[]byte(`"namespace"`),
		[]byte(`"path"`),
		[]byte(`"translation_key"`),
		[]byte(`"names"`),
	} {
		if bytes.Contains(compacted, field) {
			t.Fatalf("normalized field %s was retained: %s", field, compacted)
		}
	}
	if !bytes.Contains(compacted, []byte(`"hardness":1.5`)) ||
		!bytes.Contains(compacted, []byte(`"technical":{"solid":true}`)) {
		t.Fatalf("technical resource metadata was lost: %s", compacted)
	}
}

func TestQueueExportDocumentEntriesCompactsNormalizedNames(t *testing.T) {
	batch := newModExportWriteBatch()
	raw := []byte(`{"advancements":[{"id":"minecraft:test","namespace":"minecraft","display":{"title_names":{"zh-CN":"测试"},"description_names":{"zh-CN":"描述"},"icon":{"item":"minecraft:stone"},"frame":"task"},"criteria":{"done":{}}}]}`)
	if err := queueExportDocumentEntries(
		batch,
		catalogResourceIdentityResolver{},
		map[string]string{"minecraft": "revision"},
		"advancements/advancements.json",
		raw,
	); err != nil {
		t.Fatal(err)
	}
	if len(batch.catalogRows) != 1 {
		t.Fatalf("expected one document resource, got %d", len(batch.catalogRows))
	}
	row := batch.catalogRows[0]
	if bytes.Contains([]byte(row.Data), []byte(`"title_names"`)) {
		t.Fatalf("normalized advancement title names were retained: %s", row.Data)
	}
	if !bytes.Contains([]byte(row.Data), []byte(`"description_names":{"zh-CN":"描述"}`)) ||
		!bytes.Contains([]byte(row.Data), []byte(`"criteria":{"done":{}}`)) {
		t.Fatalf("advancement technical data was lost: %s", row.Data)
	}
	if !bytes.Contains([]byte(row.Names), []byte(`"zh-CN":"测试"`)) {
		t.Fatalf("normalized advancement names were lost: %s", row.Names)
	}
}

func TestExportDocumentEntriesNormalizesLootTableCategories(t *testing.T) {
	entries := exportDocumentEntries("worldgen/loot_tables.json", map[string]any{
		"loot_tables": []any{
			map[string]any{"id": "minecraft:blocks/stone", "path": "blocks/stone", "category": "block"},
			map[string]any{"id": "minecraft:chests/simple_dungeon", "path": "chests/simple_dungeon", "category": "chest"},
			map[string]any{"id": "minecraft:gameplay/fishing", "path": "gameplay/fishing", "category": "gameplay"},
			map[string]any{"id": "example:dispensers/test", "path": "dispensers/test", "category": "dispensers"},
		},
	})
	if len(entries) != 4 {
		t.Fatalf("expected four loot tables, got %d", len(entries))
	}
	expected := []string{"blocks", "chests", "fishing", "other"}
	for index, category := range expected {
		if actual := exportString(entries[index].Data["category"]); actual != category {
			t.Errorf("entry %d category = %q, want %q", index, actual, category)
		}
	}
}

func TestPrepareExportRegistryResourcesSupportsKeyMappingIDs(t *testing.T) {
	rows, err := prepareExportRegistryResources(
		catalogResourceIdentityResolver{},
		map[string]string{"create": "revision-create"},
		"registries/key_mappings.json",
		[]byte(`{"registry":"key_mappings","entries":[{"id":"create.keyinfo.rotate_menu","translation_key":"create.keyinfo.rotate_menu","names":{"en-US":"Rotate menu"}}]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one key mapping resource, got %d", len(rows))
	}
	row := rows[0]
	if row.RevisionID != "revision-create" || row.KindCode != "minecraft.key_mapping" ||
		row.Namespace != "create" || row.ResourcePath != "keyinfo.rotate_menu" ||
		row.RawID != "create.keyinfo.rotate_menu" {
		t.Fatalf("unexpected key mapping resource: %#v", row)
	}
}

func TestQueueExportDocumentEntriesRoutesMergedDocumentsByNamespace(t *testing.T) {
	batch := newModExportWriteBatch()
	raw := []byte(`{"biomes":[
		{"id":"minecraft:plains","namespace":"minecraft","names":{"en-US":"Plains"}},
		{"id":"examplemod:crystal_fields","namespace":"examplemod","names":{"en-US":"Crystal Fields"}}
	]}`)
	if err := queueExportDocumentEntries(
		batch,
		catalogResourceIdentityResolver{},
		map[string]string{"minecraft": "revision-minecraft", "examplemod": "revision-example"},
		"worldgen/biomes.json",
		raw,
	); err != nil {
		t.Fatal(err)
	}
	if len(batch.catalogRows) != 2 {
		t.Fatalf("expected two biome resources, got %d", len(batch.catalogRows))
	}
	revisions := map[string]string{}
	for _, row := range batch.catalogRows {
		revisions[row.RawID] = row.RevisionID
	}
	if revisions["minecraft:plains"] != "revision-minecraft" ||
		revisions["examplemod:crystal_fields"] != "revision-example" {
		t.Fatalf("merged document entries used the wrong revisions: %#v", revisions)
	}
}

func TestQueueExportTextAssetUsesBulkStage(t *testing.T) {
	batch := newModExportWriteBatch()
	if err := queueExportTextAsset(
		batch,
		"revision",
		"registries/items.json",
		[]byte(`{"entries":[{"id":"minecraft:stone"}]}`),
	); err != nil {
		t.Fatal(err)
	}
	if batch.rows != 0 || len(batch.textAssets) != 1 {
		t.Fatalf("expected one bulk text asset and no statement batch rows, got rows=%d assets=%d", batch.rows, len(batch.textAssets))
	}
	asset := batch.textAssets[0]
	if !asset.JSON || asset.ContentType != "application/json" ||
		!bytes.Contains([]byte(asset.Content), []byte(`"minecraft:stone"`)) {
		t.Fatalf("unexpected staged text asset: %#v", asset)
	}
}

func TestNormalizedExportSourceJSONRetention(t *testing.T) {
	for _, name := range []string{
		"registries/items.json",
		"recipes/recipes.json",
		"tags/tags.json",
		"worldgen/natural_generation.json",
		"block_entities/models/minecraft/chest/default.mesh.json",
		"data/minecraft/worldgen/configured_feature/ore.json",
	} {
		if retainExportTextAsset(name, nil) {
			t.Fatalf("normalized source JSON %s should not be retained", name)
		}
	}
	if !retainExportTextAsset("assets/minecraft/models/block/stone.json", nil) {
		t.Fatal("render dependency JSON should be retained")
	}
	fallbacks := map[string]struct{}{"worldgen/natural_generation.json": {}}
	if !retainExportTextAsset("worldgen/natural_generation.json", fallbacks) {
		t.Fatal("partial normalized catalog should be retained as a fallback")
	}
}

func TestCollectPartialNormalizationFallbacks(t *testing.T) {
	available := map[string]struct{}{
		"data/example/worldgen/configured_feature/ore.json": {},
	}
	result := make(map[string]struct{})
	found := collectPartialNormalizationFallbacks(map[string]any{
		"entries": []any{
			map[string]any{
				"normalization_status":               "complete",
				"configured_feature_definition_path": "data/example/worldgen/configured_feature/ignored.json",
			},
			map[string]any{
				"normalization_status":               "partial",
				"configured_feature_definition_path": "data/example/worldgen/configured_feature/ore.json",
			},
		},
	}, available, result)
	if !found {
		t.Fatal("partial normalized entry was not detected")
	}
	if _, exists := result["data/example/worldgen/configured_feature/ore.json"]; !exists {
		t.Fatalf("referenced fallback path was not retained: %#v", result)
	}
}

func TestSupportedExportTranslationFiles(t *testing.T) {
	isSupported := func(name string) bool {
		if !isExportTranslationFile(name) {
			return false
		}
		_, err := prepareExportTranslation(name, []byte(`{}`))
		return err == nil
	}
	for _, name := range []string{
		"translations/zh_cn.json",
		"translations/zh_tw.json",
		"translations/en_us.json",
		"translations/ja_jp.json",
		"translations/ru_ru.json",
		"translations/fr_fr.json",
		"translations/de_de.json",
		"translations/es_es.json",
		"translations/ko_kr.json",
		"translations/got_de.json",
		"translations/zh_hk.json",
	} {
		if !isSupported(name) {
			t.Fatalf("supported translation file %s was rejected", name)
		}
	}
	for _, name := range []string{
		"translations/languages.json",
		"translations/invalid@locale.json",
		"other/en_us.json",
	} {
		if isSupported(name) {
			t.Fatalf("unsupported translation file %s was accepted", name)
		}
	}
	translation, err := prepareExportTranslation(
		"translations/zh_cn.json",
		[]byte(`{"translation_count":1,"translations":{"item.minecraft.stone":"石头"}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if translation.Locale != "zh-CN" {
		t.Fatalf("expected BCP-47 locale zh-CN, got %s", translation.Locale)
	}
	traditionalTaiwan, err := prepareExportTranslation(
		"translations/zh_tw.json",
		[]byte(`{"translations":{"item.minecraft.stone":"石頭"}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	traditionalHongKong, err := prepareExportTranslation(
		"translations/zh_hk.json",
		[]byte(`{"translations":{"item.minecraft.stone":"石頭"}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if traditionalTaiwan.Locale != "zh-TW" || traditionalHongKong.Locale != "zh-HK" {
		t.Fatalf(
			"regional locales were collapsed: Taiwan=%s Hong Kong=%s",
			traditionalTaiwan.Locale,
			traditionalHongKong.Locale,
		)
	}
}

func TestSupportedExportNamesKeepsOnlyEditableBCP47Locales(t *testing.T) {
	names := supportedExportNames(map[string]any{
		"zh_cn": "石头",
		"en_us": "Stone",
		"ko_kr": "돌",
		"zh_hk": "石頭",
	})
	if len(names) != 2 || names["zh-CN"] != "石头" || names["en-US"] != "Stone" {
		t.Fatalf("unexpected supported names: %#v", names)
	}
	if _, exists := names["ko-KR"]; exists {
		t.Fatal("unsupported locale ko-KR was retained")
	}
}

func TestFilterSupportedExportLocalizedFieldsFiltersNestedTooltips(t *testing.T) {
	entry := map[string]any{
		"tooltips": map[string]any{
			"zh_cn": []any{"Chinese tooltip"},
			"en_us": []any{"English tooltip"},
			"ko_kr": []any{"Korean tooltip"},
		},
		"display": map[string]any{
			"description_names": map[string]any{
				"zh_tw": "Traditional Chinese description",
				"pl_pl": "Polish description",
			},
		},
	}
	filterSupportedExportLocalizedFields(entry)
	tooltips := entry["tooltips"].(map[string]any)
	if len(tooltips) != 2 || tooltips["zh-CN"] == nil || tooltips["en-US"] == nil {
		t.Fatalf("unexpected filtered tooltips: %#v", tooltips)
	}
	descriptionNames := entry["display"].(map[string]any)["description_names"].(map[string]any)
	if len(descriptionNames) != 1 || descriptionNames["zh-TW"] != "Traditional Chinese description" {
		t.Fatalf("unexpected filtered descriptions: %#v", descriptionNames)
	}
}

func TestCanonicalImportedTemplatePromotionGeometryAndIdentity(t *testing.T) {
	document, err := decodeExportJEITemplateCollection([]byte(`{
		"schema_version":"mcmods-jei-template-collection/v2","recipe_type_id":"example:machine",
		"coordinate_space":"logical_pixels","image_scale":4,
		"canvas":{"x":0,"y":0,"width":120,"height":64},"image_pixels":{"width":480,"height":256},
		"template_count":1,"templates":[{"schema_version":"mcmods-jei-layout-template/v2",
		"template_id":"template_stable","background":"recipes/jei/backgrounds/example/machine/template_stable.png",
		"slot_count":3,"slots":[
			{"slot_id":"input_0","role":"input","coordinates_available":true,"rect":{"x":4,"y":5,"width":16,"height":16}},
			{"slot_id":"byproduct_0","role":"byproduct","coordinates_available":true,"rect":{"x":90,"y":5,"width":16,"height":16}},
			{"slot_id":"decoration","role":"render_only","coordinates_available":false,"rect":{}}
		]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := canonicalImportedTemplateCanvas(document)
	if err != nil || canvas.Width != 120 || canvas.Height != 64 {
		t.Fatalf("unexpected canonical canvas: %#v, error=%v", canvas, err)
	}
	role, editable := canonicalImportedTemplateSlotRole("byproduct")
	if !editable || role != "output" {
		t.Fatalf("byproduct slot mapped to role=%q editable=%v", role, editable)
	}
	if _, editable = canonicalImportedTemplateSlotRole("render_only"); editable {
		t.Fatal("render-only source slots must not become selectable canonical slots")
	}
	rect, err := canonicalImportedTemplateSlotRect(document.Templates[0].Slots[0], canvas, 0)
	if err != nil || rect.X != 4 || rect.Y != 5 || rect.Width != 16 || rect.Height != 16 {
		t.Fatalf("unexpected canonical slot rect: %#v, error=%v", rect, err)
	}
	typeIdentity := recipeTypeIdentity(document.RecipeTypeID)
	canonicalA := catalogEditorIdentityForTemplate(typeIdentity.ID, document.Templates[0].TemplateID)
	canonicalB := catalogEditorIdentityForTemplate(typeIdentity.ID, document.Templates[0].TemplateID)
	if canonicalA.ID != canonicalB.ID {
		t.Fatalf("canonical template identity key is not stable: %#v != %#v", canonicalA, canonicalB)
	}
	if canonicalA.PublicID == canonicalB.PublicID {
		t.Fatalf("new catalog identities must receive random public IDs: %#v", canonicalA)
	}
	if exportRecipeTemplateID("revision-a", document.RecipeTypeID, document.Templates[0].TemplateID) ==
		exportRecipeTemplateID("revision-b", document.RecipeTypeID, document.Templates[0].TemplateID) {
		t.Fatal("import observation ids must remain revision scoped")
	}
	definition := canonicalImportedTemplateDefinition(document, document.Templates[0], "snapshot-a", "revision-a", "recipes/jei/templates/example/machine.json")
	if !bytes.Contains([]byte(definition), []byte(`"source":"import"`)) ||
		!bytes.Contains([]byte(definition), []byte(`"backgroundPath":"recipes/jei/backgrounds/example/machine/template_stable.png"`)) {
		t.Fatalf("canonical import metadata is incomplete: %s", definition)
	}
}

func TestValidateExportRecipeLayoutKind(t *testing.T) {
	truth := true
	falsehood := false
	tests := []struct {
		name    string
		kind    string
		ordered *bool
		valid   bool
	}{
		{name: "shaped", kind: "shaped", ordered: &truth, valid: true},
		{name: "shapeless", kind: "shapeless", ordered: &falsehood, valid: true},
		{name: "machine", kind: "not_applicable", valid: true},
		{name: "unknown", kind: "unknown", valid: true},
		{name: "shaped without order", kind: "shaped"},
		{name: "shapeless ordered", kind: "shapeless", ordered: &truth},
		{name: "machine ordered", kind: "not_applicable", ordered: &falsehood},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateExportRecipeLayoutKind(test.kind, test.ordered)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, error=%v", test.valid, err)
			}
		})
	}
}

func TestValidateExportRecipeChanceBelongsToOutputItem(t *testing.T) {
	chance := 0.4
	percent := 40.0
	valid := exportJEIBinding{SemanticRole: "output", ChanceAvailable: true, Chance: &chance, ChancePercent: &percent}
	if err := validateExportRecipeChance(valid); err != nil {
		t.Fatalf("output chance should be valid: %v", err)
	}
	invalid := valid
	invalid.SemanticRole = "input"
	if err := validateExportRecipeChance(invalid); err == nil {
		t.Fatal("input candidate must not carry output probability")
	}
	inconsistent := exportJEIBinding{SemanticRole: "output", Chance: &chance}
	if err := validateExportRecipeChance(inconsistent); err == nil {
		t.Fatal("numeric chance without chance_available must be rejected")
	}
}

func TestValidateExportBaseRecipeDocumentV2Only(t *testing.T) {
	valid := []byte(`{"schema_version":"mcmods-recipes/v2","count":1,"recipes":[{
		"id":"minecraft:oak_planks","type":"minecraft:crafting","serializer":"minecraft:crafting_shapeless",
		"source_mod_id":"minecraft","source_mod_version":"1.20.1","source_mod_id_source":"recipe_id_namespace",
		"layout_kind":"shapeless","ordered":false,"layout_classification_source":"recipe_class"
	}]}`)
	if err := validateExportBaseRecipeDocument(valid); err != nil {
		t.Fatal(err)
	}
	old := bytes.Replace(valid, []byte("mcmods-recipes/v2"), []byte("mcmods-recipes/v1"), 1)
	if err := validateExportBaseRecipeDocument(old); err == nil {
		t.Fatal("expected the legacy base recipe schema to be rejected")
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
		"schema_version": "mcmods-natural-generation/v2",
		"export_mode":    "normalized_catalog",
		"entries": []any{map[string]any{
			"id": "minecraft:ore_coal", "generation_id": "minecraft:ore_coal", "entry_kind": "placed_feature", "category": "ore",
		}},
	})
	if len(naturalGeneration) != 1 || naturalGeneration[0].ID != "minecraft:ore_coal" || naturalGeneration[0].Data["category"] != "ore" {
		t.Fatalf("unexpected natural generation projection: %#v", naturalGeneration)
	}
}

func TestValidateCatalogDocumentContractRequiresNormalizedWorldgenV2(t *testing.T) {
	validNatural := map[string]any{"schema_version": "mcmods-natural-generation/v2", "export_mode": "normalized_catalog"}
	if err := validateCatalogDocumentContract("worldgen/natural_generation.json", validNatural); err != nil {
		t.Fatalf("valid natural generation document rejected: %v", err)
	}
	if err := validateCatalogDocumentContract("worldgen/natural_generation.json", map[string]any{"schema_version": "mcmods-natural-generation/v1"}); err == nil {
		t.Fatal("legacy natural generation document should be rejected")
	}
	validStructures := map[string]any{"schema_version": "mcmods-structures/v2", "export_mode": "catalog_only", "internal_templates_exported": false}
	if err := validateCatalogDocumentContract("worldgen/structures.json", validStructures); err != nil {
		t.Fatalf("valid structures document rejected: %v", err)
	}
	invalidStructures := map[string]any{"schema_version": "mcmods-structures/v2", "export_mode": "catalog_only", "internal_templates_exported": true}
	if err := validateCatalogDocumentContract("worldgen/structures.json", invalidStructures); err == nil {
		t.Fatal("structure catalog with internal templates should be rejected")
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
	bindings, err := deriveModExportBlockBindings(files, catalogResourceIdentityResolver{}, map[string]string{"example": "revision-example"})
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

func TestShouldRestartModExportStatus(t *testing.T) {
	for _, status := range []string{"ready", "partial", "failed", "cancelled"} {
		if !shouldRestartModExportStatus(status) {
			t.Fatalf("expected terminal status %s to start a fresh import run", status)
		}
	}
	for _, status := range []string{"queued", "validating", "importing"} {
		if shouldRestartModExportStatus(status) {
			t.Fatalf("expected active status %s to remain deduplicated", status)
		}
	}
}

func TestRecipeIdentityScope(t *testing.T) {
	canonicalA := recipeIdentity("package-a", "minecraft:crafting", "example:recipe", true)
	canonicalB := recipeIdentity("package-b", "minecraft:crafting", "example:recipe", true)
	if canonicalA.ID != canonicalB.ID {
		t.Fatalf("authoritative recipe identity key changed across packages: %#v != %#v", canonicalA, canonicalB)
	}
	generatedA := recipeIdentity("package-a", "example:machine", "example:machine/__generated/recipe_000001", false)
	generatedB := recipeIdentity("package-b", "example:machine", "example:machine/__generated/recipe_000001", false)
	if generatedA.ID == generatedB.ID {
		t.Fatalf("generated recipe identity must remain package scoped: %#v", generatedA)
	}
}
