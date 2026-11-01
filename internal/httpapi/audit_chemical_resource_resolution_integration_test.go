package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image/color"
	"io"
	"strings"
	"testing"
)

type auditChemicalKind struct{ suffix, ingredient, class, code string }

var auditChemicalKinds = []auditChemicalKind{
	{"gas", "mekanism_gas", "mekanism.api.chemical.gas.GasStack", "mekanism.gas"},
	{"infusion", "mekanism_infuse_type", "mekanism.api.chemical.infuse.InfusionStack", "mekanism.infusion"},
	{"pigment", "mekanism_pigment", "mekanism.api.chemical.pigment.PigmentStack", "mekanism.pigment"},
	{"slurry", "mekanism_slurry", "mekanism.api.chemical.slurry.SlurryStack", "mekanism.slurry"},
}

func auditChemicalSharedCanonicalZIP(t *testing.T) []byte {
	t.Helper()
	base := auditRecipeGeometryAssetZIP(t)
	archive, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, file := range archive.File {
		if strings.HasPrefix(file.Name, "ingredients/") || strings.HasPrefix(file.Name, "icons/") {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name] = string(raw)
	}
	files["registries/items.json"] = `{"registry":"items","entries":[]}`
	documents := map[string]map[string]any{}
	for _, path := range []string{"recipes/jei/categories.json", "recipes/jei/recipes/example/pressing.json"} {
		var doc map[string]any
		if err := json.Unmarshal([]byte(files[path]), &doc); err != nil {
			t.Fatal(err)
		}
		documents[path] = doc
	}
	category := documents["recipes/jei/categories.json"]
	collection := documents["recipes/jei/recipes/example/pressing.json"]
	recipeSource, _ := json.Marshal(collection["recipes"].([]any)[0])
	summarySource, _ := json.Marshal(category["recipes"].([]any)[0])
	recipes, summaries, types := []any{}, []any{}, []any{}
	for index, kind := range auditChemicalKinds {
		var recipe, summary map[string]any
		if err := json.Unmarshal(recipeSource, &recipe); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(summarySource, &summary); err != nil {
			t.Fatal(err)
		}
		id := "example:collision_" + kind.suffix
		recipe["recipe_id"], recipe["recipe_key"], summary["recipe_id"], summary["recipe_key"] = id, id, id, id
		recipe["bindings"].([]any)[1].(map[string]any)["alternatives"] = []any{map[string]any{"type": kind.class, "resource_location": "example:chemical_shared", "amount": 1000 + index}}
		recipes, summaries = append(recipes, recipe), append(summaries, summary)
		uniqueID := "example:chemical_shared|" + kind.suffix
		icon := "ingredients/" + strings.ToLower(kind.class) + "/32/example/chemical_shared--" + sha256Hex([]byte(uniqueID))[:16] + ".png"
		files[icon] = string(auditGeometryPNG(t, 32, 32, color.NRGBA{R: uint8(80 + index*32), G: uint8(50 + index*20), B: 200, A: uint8(128 + index*16)}))
		types = append(types, map[string]any{"ingredient_type": kind.class, "ingredient_kind": kind.ingredient, "path_key": strings.ToLower(kind.class), "count": 1, "entries": []any{map[string]any{"ingredient_kind": kind.ingredient, "resource_location": "example:chemical_shared", "unique_id": uniqueID, "amount": 1000 + index, "names": map[string]string{"en-US": "Synthetic " + kind.suffix}, "tags": []any{}, "icons": map[string]string{"32": icon}}}})
	}
	collection["count"], collection["recipes"] = 4, recipes
	category["recipe_count"], category["layout_sample_count"], category["recipes"] = 4, 4, summaries
	group := category["categories"].([]any)[0].(map[string]any)
	for _, key := range []string{"source_recipe_count", "recipe_count", "exported_recipe_count"} {
		group[key] = 4
	}
	filter := category["filter_summary"].(map[string]any)
	for _, key := range []string{"discovered_recipe_presentations", "exported_recipe_presentations", "layout_sample_presentations"} {
		filter[key] = 4
	}
	for path, doc := range documents {
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = string(raw)
	}
	raw, err := json.Marshal(map[string]any{"schema_version": "mcmods-ingredients/v1", "type_count": 4, "ingredient_count": 4, "types": types})
	if err != nil {
		t.Fatal(err)
	}
	files["ingredients/ingredients.json"] = string(raw)
	return auditFixGRZip(t, files)
}

func TestAuditF002FourChemicalKindsResolveTheirOwnCanonicalIdentityAndImageHTTPPostgresIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	_, revision := f.imported(t, auditChemicalSharedCanonicalZIP(t))
	f.reviewGR(t, revision, "approved")
	seen := map[string]bool{}
	for index, kind := range auditChemicalKinds {
		var publicID, recipeID, iconPath string
		if err := f.db.QueryRow(f.ctx, `select entity.public_id,snapshot.icon_path from game_resources resource
            join catalog_entities entity on entity.id=resource.entity_id join resource_import_snapshots snapshot on snapshot.resource_id=resource.entity_id
            where resource.kind_code=$1 and resource.canonical_id='example:chemical_shared' and snapshot.revision_id=$2`, kind.code, revision).Scan(&publicID, &iconPath); err != nil {
			t.Fatal(err)
		}
		if publicID == "" || seen[publicID] || iconPath == "" {
			t.Fatal("different chemical kinds shared a canonical identity or lost source icon")
		}
		seen[publicID] = true
		resolved, err := f.server.resolveExportResources(f.ctx, []exportResourceKey{{RevisionID: revision, ResourceID: "example:chemical_shared", Kind: kind.ingredient}})
		if err != nil {
			t.Fatal(err)
		}
		source := resolved[exportResourceKey{RevisionID: revision, ResourceID: "example:chemical_shared", Kind: normalizeExportResourceKind(kind.ingredient)}]
		if source.PublicID != publicID || source.KindCode != kind.code || source.IconPath != iconPath || source.RevisionID != revision {
			t.Fatalf("supported %s canonical source/image missing or cross-kind: got public=%s kind=%s icon=%s expected=%s/%s", kind.ingredient, source.PublicID, source.KindCode, source.IconPath, publicID, kind.code)
		}
		fallbackKey := exportResourceKey{ResourceID: "example:chemical_shared", Kind: kind.suffix}
		fallback, err := f.server.resolveManualModContentResources(f.ctx, []exportResourceKey{fallbackKey})
		if err != nil {
			t.Fatal(err)
		}
		if candidate := fallback[fallbackKey]; candidate.PublicID != publicID || candidate.KindCode != kind.code || candidate.VersionPublicID != f.versionPublic {
			t.Fatalf("typed canonical fallback crossed chemical identities for %s: %#v", kind.code, candidate)
		}
		if err := f.db.QueryRow(f.ctx, `select entity.public_id from recipes recipe join catalog_entities entity on entity.id=recipe.entity_id where recipe.canonical_source_id=$1`, "example:collision_"+kind.suffix).Scan(&recipeID); err != nil {
			t.Fatal(err)
		}
		render := decodeTEST022Data[map[string]any](t, f.require(t, "", "GET", "/api/v1/catalog/recipes/"+recipeID+"/render", nil, 200))
		output := render["layout"].(map[string]any)["slots"].([]any)[1].(map[string]any)["alternatives"].([]any)[0].(map[string]any)
		if output["publicId"] != publicID || output["kind"] != kind.code || output["iconPath"] != iconPath || output["sourceRevisionId"] != revision || output["ingredient_kind"] != kind.ingredient || output["amount"] != float64(1000+index) {
			t.Fatalf("actual anonymous recipe projection loses typed %s reference: %#v", kind.code, output)
		}
	}
	unrelated, err := f.server.resolveExportResources(f.ctx, []exportResourceKey{{RevisionID: revision, ResourceID: "example:chemical_shared", Kind: "item"}})
	if err != nil || len(unrelated) != 0 {
		t.Fatalf("chemical canonical ID resolved as unrelateditem: %#v/%v", unrelated, err)
	}
	t.Log("actual HTTP/Worker/independent review resolves four same-canonical chemical IDs to four distinct own public identities/icons, including producer infuse_type alias; no family-level chemical wildcard")
}
