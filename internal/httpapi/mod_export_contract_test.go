package httpapi

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the public wire contract with the hash-pinned game outputs shipped
// in this repository. Optional larger packages use the same import code.
func TestExporterSamplePackageContracts(t *testing.T) {
	t.Run("full", func(t *testing.T) {
		checkExporterSamplePackage(t, auditRecipeScopePackage(t, false))
	})
	t.Run("templates-only", func(t *testing.T) {
		checkExporterSamplePackage(t, auditRecipeScopePackage(t, true))
	})
	directory := strings.TrimSpace(os.Getenv("MCMODS_EXPORT_TEST_DIR"))
	if directory == "" {
		return
	}
	archives, err := filepath.Glob(filepath.Join(directory, "*.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) == 0 {
		t.Fatal("no exporter ZIP packages found")
	}
	for _, archive := range archives {
		t.Run(filepath.Base(archive), func(t *testing.T) {
			reader, err := zip.OpenReader(archive)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			files, err := validateExportZIP(reader.File)
			if err != nil {
				t.Fatal(err)
			}
			checkExporterSamplePackage(t, files)
		})
	}
}

func TestExportersForSupportedLoadersMatchRecordedPackages(t *testing.T) {
	const directory = "testdata/game-20261121"
	raw, err := os.ReadFile(filepath.Join(directory, "provenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var provenance struct {
		Minecraft, Namespace string
		Samples              []struct {
			Loader    string
			ZIPHash   string `json:"zip_sha256"`
			InputHash string `json:"input_sha256"`
		}
	}
	if err := json.Unmarshal(raw, &provenance); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, sample := range provenance.Samples {
		if (sample.Loader != "forge" && sample.Loader != "fabric") || seen[sample.Loader] {
			t.Fatalf("unexpected game sample loader %q", sample.Loader)
		}
		seen[sample.Loader] = true
		t.Run(sample.Loader, func(t *testing.T) {
			readPinned := func(name, digest string) []byte {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(directory, sample.Loader, name))
				if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != digest {
					t.Fatalf("recorded %s does not match its game provenance: %v", name, err)
				}
				return data
			}
			readPinned("governancefixture.jar", sample.InputHash)
			archive := readPinned("export.zip", sample.ZIPHash)
			reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
			if err != nil {
				t.Fatal(err)
			}
			files, err := validateExportZIP(reader.File)
			if err != nil {
				t.Fatal(err)
			}
			var manifest modExportManifest
			if err := json.Unmarshal(auditRecipeScopeRead(t, files, "manifest.json"), &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.Loader != sample.Loader || manifest.MinecraftVersion != provenance.Minecraft || len(manifest.Configuration.Namespaces) != 1 || manifest.Configuration.Namespaces[0] != provenance.Namespace || manifest.Configuration.AllNamespaces {
				t.Fatal("recorded game, loader or namespace disagrees with the package")
			}
			checkExporterSamplePackage(t, files)
		})
	}
	if len(seen) != 2 {
		t.Fatal("each supported loader must have a recorded game package")
	}
}

func checkExporterSamplePackage(t *testing.T, files map[string]*zip.File) {
	t.Helper()
	readMetadata := func(name string) []byte {
		t.Helper()
		if files[name] == nil {
			t.Fatalf("missing %s", name)
		}
		raw, err := readExportZIPFile(files[name], 4<<20)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	var manifest modExportManifest
	if err := json.Unmarshal(readMetadata("manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	if err := validateModExportManifest(manifest); err != nil {
		t.Fatal(err)
	}
	revisions := make(map[string]string)
	for _, namespace := range normalizeExportNamespaces(manifest.Configuration.Namespaces) {
		revisions[namespace] = "sample-revision-" + namespace
	}
	if _, err := decodeModExportCapabilities(readMetadata(modExportCapabilitiesPath), manifest); err != nil {
		t.Fatal(err)
	}
	if files["registries/blocks.json"] != nil && files["registries/items.json"] != nil {
		blocks, err := readExportRegistryLinks(files["registries/blocks.json"])
		if err != nil {
			t.Fatal(err)
		}
		bindings, err := deriveModExportBlockBindings(files, catalogResourceIdentityResolver{}, revisions)
		if err != nil {
			t.Fatal(err)
		}
		expected := 0
		for _, block := range blocks {
			if revisions[strings.ToLower(block.Namespace)] != "" {
				expected++
			}
		}
		if len(bindings) != expected {
			t.Fatalf("expected %d import-time block bindings, got %d", expected, len(bindings))
		}
	}
	if files[modExportBlockEntityIndexPath] != nil {
		var index exportBlockEntityIndex
		if err := json.Unmarshal(auditRecipeScopeRead(t, files, modExportBlockEntityIndexPath), &index); err != nil {
			t.Fatal(err)
		}
		models, err := deriveModExportBlockEntityModels(files, catalogResourceIdentityResolver{}, revisions)
		if err != nil {
			t.Fatal(err)
		}
		if len(models) != index.Count {
			t.Fatalf("block entity index declares %d models, derived %d", index.Count, len(models))
		}
	}
	if err := validateExportBaseRecipeDocument(auditRecipeScopeRead(t, files, "recipes/recipes.json")); err != nil {
		t.Fatal(err)
	}
	for name, file := range files {
		if strings.HasPrefix(name, "translations/") && strings.HasSuffix(name, ".json") && path.Base(name) != "languages.json" && !file.FileInfo().IsDir() {
			if _, _, _, err := decodeExportTranslationValues(auditRecipeScopeRead(t, files, name)); err != nil {
				t.Fatalf("decode %s: %v", name, err)
			}
		}
	}

	scope, err := newExportRecipeImportScope(revisions, auditRecipeScopeRead(t, files, "recipes/jei/categories.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, recipe := range scope.document.Recipes {
		if err := validateExportRecipeLayoutKind(recipe.LayoutKind, recipe.Ordered); err != nil {
			t.Fatalf("recipe index %s: %v", recipe.RecipeKey, err)
		}
	}
	declared := make(map[string]bool)
	for _, category := range scope.document.Categories {
		declared[category.TemplateCollection] = true
		declared[category.RecipeCollection] = true
		checkExporterCategoryCollections(t, files, scope, category)
	}
	for name := range files {
		if (strings.HasPrefix(name, "recipes/jei/templates/") || strings.HasPrefix(name, "recipes/jei/recipes/")) && strings.HasSuffix(name, ".json") && !declared[name] {
			t.Fatalf("undeclared JEI collection %s", name)
		}
	}
}

func checkExporterCategoryCollections(t *testing.T, files map[string]*zip.File, scope *exportRecipeImportScope, category exportJEICategory) {
	t.Helper()
	templates, err := decodeExportJEITemplateCollection(auditRecipeScopeRead(t, files, category.TemplateCollection))
	if err != nil {
		t.Fatal(err)
	}
	recipes, err := decodeExportJEIRecipeCollection(auditRecipeScopeRead(t, files, category.RecipeCollection))
	if err != nil {
		t.Fatal(err)
	}
	if templates.RecipeTypeID != category.RecipeTypeID || recipes.RecipeTypeID != category.RecipeTypeID || recipes.TemplateCollection != category.TemplateCollection {
		t.Fatalf("%s: category, template and recipe collection references disagree", category.RecipeTypeID)
	}
	if len(templates.Templates) != category.TemplateCount || len(recipes.Recipes) != category.ExportedRecipeCount || (category.RecipesOmitted && len(recipes.Recipes) != 0) {
		t.Fatalf("%s: template or recipe count disagrees with category index", category.RecipeTypeID)
	}
	batch := newModExportWriteBatch()
	if err := scope.queueTemplates(batch, category.TemplateCollection, templates); err != nil {
		t.Fatal(err)
	}
	if err := scope.queueRecipes(batch, catalogResourceIdentityResolver{}, "sample-package", category.RecipeCollection, recipes); err != nil {
		t.Fatal(err)
	}

	slotsByTemplate := make(map[string]map[string]bool)
	for _, template := range templates.Templates {
		slots := make(map[string]bool)
		for _, slot := range template.Slots {
			slots[slot.SlotID] = true
		}
		slotsByTemplate[template.TemplateID] = slots
	}
	for _, recipe := range recipes.Recipes {
		slots, ok := slotsByTemplate[recipe.TemplateID]
		if !ok {
			t.Fatalf("recipe %s references missing template %s", recipe.RecipeKey, recipe.TemplateID)
		}
		for _, binding := range recipe.Bindings {
			if !slots[binding.SlotID] {
				t.Fatalf("recipe %s references missing template slot %s", recipe.RecipeKey, binding.SlotID)
			}
		}
	}
}
