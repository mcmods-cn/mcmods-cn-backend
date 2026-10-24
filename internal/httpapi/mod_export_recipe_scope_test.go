package httpapi

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func auditRecipeScopePackage(t *testing.T, templateOnly bool) map[string]*zip.File {
	t.Helper()
	name, hash := "auditfixture-minecraft1201-fabric-jei1520-v7.zip", "059b1c56921a3f70b934bd577fd940486982da03b24d2f6dca7c0efcfb5af2eb"
	if templateOnly {
		name, hash = "auditfixture-minecraft1201-fabric-jei1520-template-only-v7.zip", "95bc42266d526b8b91a23c13da8af4cb75c207b3fa73280ac7463b139893b431"
	}
	raw, err := os.ReadFile("testdata/audit-20261024/" + name)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != hash {
		t.Fatal("original actual-game archive bytes changed")
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	files, err := validateExportZIP(reader.File)
	if err != nil {
		t.Fatal(err)
	}
	return files
}
func auditRecipeScopeRead(t *testing.T, files map[string]*zip.File, name string) []byte {
	t.Helper()
	f := files[name]
	if f == nil {
		t.Fatalf("missing actual-game document %s", name)
	}
	raw, err := readExportZIPFile(f, maxExportJSONSize)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func auditRecipeScopeDocument(t *testing.T, files map[string]*zip.File) exportJEICategoryDocument {
	t.Helper()
	var d exportJEICategoryDocument
	if err := json.Unmarshal(auditRecipeScopeRead(t, files, "recipes/jei/categories.json"), &d); err != nil {
		t.Fatal(err)
	}
	return d
}
func auditRecipeScopeRaw(t *testing.T, d exportJEICategoryDocument) []byte {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAuditRecipeScopeActualGameSharedCategoryRetainsOwner(t *testing.T) {
	files := auditRecipeScopePackage(t, false)
	revisions := map[string]string{"auditfixture": "revision-actual-owner"}
	scope, err := newExportRecipeImportScope(revisions, auditRecipeScopeRead(t, files, "recipes/jei/categories.json"))
	if err != nil {
		t.Fatal(err)
	}
	batch := newModExportWriteBatch()
	templates, err := decodeExportJEITemplateCollection(auditRecipeScopeRead(t, files, "recipes/jei/templates/minecraft/crafting.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = scope.queueTemplates(batch, "recipes/jei/templates/minecraft/crafting.json", templates); err != nil {
		t.Fatal(err)
	}
	recipes, err := decodeExportJEIRecipeCollection(auditRecipeScopeRead(t, files, "recipes/jei/recipes/minecraft/crafting.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = scope.queueRecipes(batch, catalogResourceIdentityResolver{}, "package-actual", "recipes/jei/recipes/minecraft/crafting.json", recipes); err != nil {
		t.Fatal(err)
	}
	if len(batch.recipes) != 2 {
		t.Fatalf("actual recipes=%d", len(batch.recipes))
	}
	for _, r := range batch.recipes {
		if r.RevisionID != "revision-actual-owner" || r.SourceModID != "auditfixture" || r.RecipeTypeID != "minecraft:crafting" || !strings.HasPrefix(r.SourceRecipeID, "auditfixture:") {
			t.Fatalf("source/category identity lost: %+v", r)
		}
	}
	if len(revisions) != 1 || revisions["minecraft"] != "" {
		t.Fatal("dependency scope widened the admitted namespace map")
	}
	if _, err = exportRevisionForRecipeType(revisions, "minecraft:crafting"); err == nil {
		t.Fatal("strict general namespace helper was bypassed")
	}
}
func TestAuditRecipeScopeRejectsForgedOrUnadmittedOwners(t *testing.T) {
	d := auditRecipeScopeDocument(t, auditRecipeScopePackage(t, false))
	for _, tc := range []struct {
		name   string
		mutate func(*exportJEICategoryDocument)
	}{
		{"spoof-source", func(x *exportJEICategoryDocument) { x.Recipes[0].SourceModID = "foreign" }},
		{"unadmitted-canonical-owner", func(x *exportJEICategoryDocument) {
			x.Recipes[0].RecipeID = "foreign:recipe"
			x.Recipes[0].SourceModID = "foreign"
		}},
		{"canonical-type-attribution", func(x *exportJEICategoryDocument) { x.Recipes[0].SourceModIDSource = "recipe_type_namespace" }},
		{"undeclared-category", func(x *exportJEICategoryDocument) { x.Recipes[0].RecipeTypeID = "minecraft:undeclared" }},
		{"duplicate-index", func(x *exportJEICategoryDocument) { x.Recipes = append(x.Recipes, x.Recipes[0]) }},
		{"wrong-origin", func(x *exportJEICategoryDocument) { x.Recipes[0].OriginKind = "jei_generated_recipe" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := auditRecipeScopeRaw(t, d)
			var mutated exportJEICategoryDocument
			if err := json.Unmarshal(raw, &mutated); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&mutated)
			if _, err := newExportRecipeImportScope(map[string]string{"auditfixture": "only-admitted"}, auditRecipeScopeRaw(t, mutated)); err == nil {
				t.Fatal("invalid recipe provenance was admitted")
			}
		})
	}
}
func TestAuditRecipeScopeActualTemplateOnlyKeepsDependencyObservation(t *testing.T) {
	files := auditRecipeScopePackage(t, true)
	scope, err := newExportRecipeImportScope(map[string]string{"auditfixture": "only-admitted"}, auditRecipeScopeRead(t, files, "recipes/jei/categories.json"))
	if err != nil {
		t.Fatal(err)
	}
	templates, err := decodeExportJEITemplateCollection(auditRecipeScopeRead(t, files, "recipes/jei/templates/minecraft/crafting.json"))
	if err != nil {
		t.Fatal(err)
	}
	batch := newModExportWriteBatch()
	if err = scope.queueTemplates(batch, "recipes/jei/templates/minecraft/crafting.json", templates); err != nil {
		t.Fatal(err)
	}
	if batch.rows == 0 || len(scope.indexed) != 0 {
		t.Fatal("template-only observation was dropped or invented recipes")
	}
}
func TestAuditRecipeScopeSharedCategorySeparatesControlledMultipleOwners(t *testing.T) {
	files := auditRecipeScopePackage(t, false)
	d := auditRecipeScopeDocument(t, files)
	bindings, err := decodeExportJEIRecipeCollection(auditRecipeScopeRead(t, files, "recipes/jei/recipes/minecraft/crafting.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i, owner := range []string{"alpha", "beta"} {
		d.Recipes[i].RecipeID = owner + ":recipe"
		d.Recipes[i].RecipeKey = owner + "-key"
		d.Recipes[i].SourceModID = owner
		bindings.Recipes[i].RecipeID = d.Recipes[i].RecipeID
		bindings.Recipes[i].RecipeKey = d.Recipes[i].RecipeKey
		bindings.Recipes[i].SourceModID = owner
	}
	scope, err := newExportRecipeImportScope(map[string]string{"alpha": "revision-alpha", "beta": "revision-beta"}, auditRecipeScopeRaw(t, d))
	if err != nil {
		t.Fatal(err)
	}
	batch := newModExportWriteBatch()
	if err = scope.queueRecipes(batch, catalogResourceIdentityResolver{}, "controlled-multi-owner", d.Categories[0].RecipeCollection, bindings); err != nil {
		t.Fatal(err)
	}
	if len(batch.recipes) != 2 {
		t.Fatalf("recipes=%d", len(batch.recipes))
	}
	for _, r := range batch.recipes {
		if r.RevisionID != "revision-"+r.SourceModID || r.RecipeTypeID != "minecraft:crafting" {
			t.Fatal("shared category transferred recipe ownership")
		}
	}
}

func TestAuditRecipeScopeBindingsMustMatchDeclaredSourceIndex(t *testing.T) {
	files := auditRecipeScopePackage(t, false)
	scope, err := newExportRecipeImportScope(map[string]string{"auditfixture": "admitted-owner"}, auditRecipeScopeRead(t, files, "recipes/jei/categories.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, want string
		mutate     func(*exportJEIRecipeBinding)
	}{
		{"owner", "disagrees", func(r *exportJEIRecipeBinding) { r.SourceModID = "foreign" }},
		{"canonical-id", "disagrees", func(r *exportJEIRecipeBinding) { r.RecipeID = "foreign:recipe" }},
		{"template-id", "disagrees", func(r *exportJEIRecipeBinding) { r.TemplateID = "template_fedcba9876543210" }},
		{"unindexed-key", "no matching layout index", func(r *exportJEIRecipeBinding) { r.RecipeKey = "unindexed" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document, err := decodeExportJEIRecipeCollection(auditRecipeScopeRead(t, files, "recipes/jei/recipes/minecraft/crafting.json"))
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(&document.Recipes[0])
			batch := newModExportWriteBatch()
			err = scope.queueRecipes(batch, catalogResourceIdentityResolver{}, "package", "recipes/jei/recipes/minecraft/crafting.json", document)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wrong rejection: %v", err)
			}
			if len(batch.recipes) != 0 || batch.rows != 0 {
				t.Fatal("rejected first binding queued writes")
			}
		})
	}
}
func TestAuditRecipeScopeGeneratedIdentityUsesItsDeclaredTypeOwner(t *testing.T) {
	d := auditRecipeScopeDocument(t, auditRecipeScopePackage(t, false))
	d.Categories[0].RecipeTypeID = "auditfixture:machine"
	generated := false
	for i := range d.Recipes {
		r := &d.Recipes[i]
		r.RecipeTypeID = "auditfixture:machine"
		r.RecipeID = fmt.Sprintf("auditfixture:machine/__generated/recipe_%06d", i)
		r.RecipeIDSource = "generated_index"
		r.RecipeIDCanonical = &generated
		r.OriginKind = "jei_generated_recipe"
		r.SourceModIDSource = "recipe_type_namespace"
	}
	scope, err := newExportRecipeImportScope(map[string]string{"auditfixture": "generated-owner"}, auditRecipeScopeRaw(t, d))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := scope.owners["auditfixture:machine"]["auditfixture"]; !ok {
		t.Fatal("legitimate noncanonical provenance lost")
	}
}
