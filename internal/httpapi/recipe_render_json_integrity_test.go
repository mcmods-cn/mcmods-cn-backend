package httpapi

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestRecipeImportRejectsNonObjectJSONFields(t *testing.T) {
	for _, value := range []string{"null", "[]", `"text"`, "1"} {
		recipeDocument := fmt.Sprintf(`{
			"schema_version":"mcmods-jei-recipe-collection/v2",
			"recipe_type_id":"minecraft:crafting",
			"template_collection":"recipes/jei/templates/crafting.json",
			"count":1,
			"recipes":[{
				"parameters":%s,
				"bindings":[{"chance_texts":{"en_us":"Chance"}}]
			}]
		}`, value)
		if document, err := decodeExportJEIRecipeCollection([]byte(recipeDocument)); err == nil {
			t.Fatalf("recipe parameters %s were accepted as %#v", value, document.Recipes[0].Parameters)
		}

		chanceDocument := fmt.Sprintf(`{
			"schema_version":"mcmods-jei-recipe-collection/v2",
			"recipe_type_id":"minecraft:crafting",
			"template_collection":"recipes/jei/templates/crafting.json",
			"count":1,
			"recipes":[{
				"parameters":{},
				"bindings":[{"chance_texts":%s}]
			}]
		}`, value)
		if document, err := decodeExportJEIRecipeCollection([]byte(chanceDocument)); err == nil {
			t.Fatalf("recipe chance_texts %s were accepted as %#v", value, document.Recipes[0].Bindings[0].ChanceTexts)
		}
	}
}

func TestRecipeTemplateImportRejectsNonObjectLayoutFields(t *testing.T) {
	for _, field := range []string{"canvas", "image_pixels", "content"} {
		for _, raw := range []string{"null", "[]", `"text"`, "1"} {
			document := fmt.Sprintf(`{
				"schema_version":"mcmods-jei-template-collection/v2",
				"recipe_type_id":"minecraft:crafting",
				"coordinate_space":"logical_pixels",
				"image_scale":1,
				"%s":%s,
				"template_count":0,
				"templates":[]
			}`, field, raw)
			if value, err := decodeExportJEITemplateCollection([]byte(document)); err == nil {
				t.Fatalf("template %s value %s was accepted as %#v", field, raw, value)
			}
		}
	}

	for _, field := range []string{"rect", "visual_rect"} {
		for _, raw := range []string{"null", "[]", `"text"`, "1"} {
			document := fmt.Sprintf(`{
				"schema_version":"mcmods-jei-template-collection/v2",
				"recipe_type_id":"minecraft:crafting",
				"coordinate_space":"logical_pixels",
				"image_scale":1,
				"template_count":1,
				"templates":[{"slot_count":1,"slots":[{"%s":%s}]}]
			}`, field, raw)
			if value, err := decodeExportJEITemplateCollection([]byte(document)); err == nil {
				t.Fatalf("template slot %s value %s was accepted as %#v", field, raw, value)
			}
		}
	}
}

func TestDecodeRecipeJSONObjectRejectsMalformedAndNonObjectValues(t *testing.T) {
	for _, raw := range []string{"", "{", "null", "[]", `"text"`, "1"} {
		if value, err := decodeRecipeJSONObject([]byte(raw), "test field"); err == nil {
			t.Fatalf("recipe object value %q was accepted as %#v", raw, value)
		}
	}
	value, err := decodeRecipeJSONObject([]byte(`{"group":"building"}`), "test field")
	if err != nil {
		t.Fatalf("valid recipe object failed: %v", err)
	}
	if value["group"] != "building" {
		t.Fatalf("valid recipe object = %#v", value)
	}
}

func TestRecipeRenderSourceDoesNotDiscardJSONObjectErrors(t *testing.T) {
	raw, err := os.ReadFile("recipe_render_layout.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	if strings.Contains(source, "_ = json.Unmarshal(raw, &result)") {
		t.Fatal("recipe render decoder still discards JSON errors")
	}
	for _, required := range []string{"decodeStoredJSONObject", "decode recipe", "return nil, fmt.Errorf"} {
		if !strings.Contains(source, required) {
			t.Fatalf("recipe render error propagation is missing %q", required)
		}
	}
}
