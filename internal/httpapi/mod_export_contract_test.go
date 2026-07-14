package httpapi

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// This test is opt-in because the real exporter packages are large and live
// outside the repository. It validates the package contract without requiring
// PostgreSQL or OSS.
func TestExporterSamplePackageContracts(t *testing.T) {
	directory := strings.TrimSpace(os.Getenv("MCMODS_EXPORT_TEST_DIR"))
	if directory == "" {
		t.Skip("MCMODS_EXPORT_TEST_DIR is not configured")
	}
	archives, err := filepath.Glob(filepath.Join(directory, "*.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) == 0 {
		t.Fatal("no exporter ZIP packages found")
	}
	for _, archive := range archives {
		archive := archive
		t.Run(filepath.Base(archive), func(t *testing.T) {
			reader, openErr := zip.OpenReader(archive)
			if openErr != nil {
				t.Fatal(openErr)
			}
			defer reader.Close()
			files, validateErr := validateExportZIP(reader.File)
			if validateErr != nil {
				t.Fatal(validateErr)
			}
			manifestRaw, readErr := readExportZIPFile(files["manifest.json"], 4<<20)
			if readErr != nil {
				t.Fatal(readErr)
			}
			var manifest modExportManifest
			if unmarshalErr := json.Unmarshal(manifestRaw, &manifest); unmarshalErr != nil {
				t.Fatal(unmarshalErr)
			}
			if validateErr = validateModExportManifest(manifest); validateErr != nil {
				t.Fatal(validateErr)
			}
			capabilityFile := files[modExportCapabilitiesPath]
			if capabilityFile == nil {
				t.Fatalf("%s is missing", modExportCapabilitiesPath)
			}
			capabilityRaw, readErr := readExportZIPFile(capabilityFile, 4<<20)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if _, decodeErr := decodeModExportCapabilities(capabilityRaw, manifest); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if files["registries/blocks.json"] != nil && files["registries/items.json"] != nil {
				blocks, blockErr := readExportRegistryLinks(files["registries/blocks.json"])
				if blockErr != nil {
					t.Fatal(blockErr)
				}
				revisions := make(map[string]string)
				for _, namespace := range normalizeExportNamespaces(manifest.Configuration.Namespaces) {
					revisions[namespace] = "sample-revision-" + namespace
				}
				if len(revisions) == 0 {
					for _, block := range blocks {
						revisions[strings.ToLower(block.Namespace)] = "sample-revision-" + strings.ToLower(block.Namespace)
					}
				}
				bindings, bindingErr := deriveModExportBlockBindings(files, revisions)
				if bindingErr != nil {
					t.Fatal(bindingErr)
				}
				expectedBindings := 0
				for _, block := range blocks {
					if revisions[strings.ToLower(block.Namespace)] != "" {
						expectedBindings++
					}
				}
				if len(bindings) != expectedBindings {
					t.Fatalf("expected %d import-time block bindings, got %d", expectedBindings, len(bindings))
				}
			}

			categoryFile := files["recipes/jei/categories.json"]
			if categoryFile == nil {
				t.Fatal("package contains no JEI v5 category index")
			}
			categoryRaw, readErr := readExportZIPFile(categoryFile, maxExportJSONSize)
			if readErr != nil {
				t.Fatal(readErr)
			}
			var categoryDocument exportJEICategoryDocument
			if decodeErr := json.Unmarshal(categoryRaw, &categoryDocument); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if categoryDocument.SchemaVersion != "mcmods-jei-categories/v5" {
				t.Fatalf("unexpected JEI category schema %q", categoryDocument.SchemaVersion)
			}
			for _, recipe := range categoryDocument.Recipes {
				if validateErr := validateExportRecipeLayoutKind(recipe.LayoutKind, recipe.Ordered); validateErr != nil {
					t.Fatalf("recipe index %s: %v", recipe.RecipeKey, validateErr)
				}
			}
			templatesByType := make(map[string]map[string]struct{})
			templateFiles := 0
			recipeFiles := 0
			recipeRows := 0
			recipeDocuments := make([]exportJEIRecipeCollection, 0)
			for name, file := range files {
				if file.FileInfo().IsDir() || !strings.HasSuffix(name, ".json") {
					continue
				}
				if !strings.HasPrefix(name, "recipes/jei/templates/") &&
					!strings.HasPrefix(name, "recipes/jei/recipes/") &&
					!(strings.HasPrefix(name, "translations/") && path.Base(name) != "languages.json") {
					continue
				}
				raw, fileErr := readExportZIPFile(file, maxExportJSONSize)
				if fileErr != nil {
					t.Fatalf("read %s: %v", name, fileErr)
				}
				if strings.HasPrefix(name, "translations/") {
					if _, _, _, decodeErr := decodeExportTranslationValues(raw); decodeErr != nil {
						t.Fatalf("decode %s: %v", name, decodeErr)
					}
					continue
				}
				if strings.HasPrefix(name, "recipes/jei/templates/") {
					document, decodeErr := decodeExportJEITemplateCollection(raw)
					if decodeErr != nil {
						t.Fatalf("decode %s: %v", name, decodeErr)
					}
					ids := templatesByType[document.RecipeTypeID]
					if ids == nil {
						ids = make(map[string]struct{})
						templatesByType[document.RecipeTypeID] = ids
					}
					for _, template := range document.Templates {
						ids[template.TemplateID] = struct{}{}
					}
					templateFiles++
					continue
				}
				document, decodeErr := decodeExportJEIRecipeCollection(raw)
				if decodeErr != nil {
					t.Fatalf("decode %s: %v", name, decodeErr)
				}
				recipeDocuments = append(recipeDocuments, document)
				recipeFiles++
				recipeRows += len(document.Recipes)
			}
			for _, document := range recipeDocuments {
				for _, recipe := range document.Recipes {
					if validateErr := validateExportRecipeLayoutKind(recipe.LayoutKind, recipe.Ordered); validateErr != nil {
						t.Fatalf("recipe %s: %v", recipe.RecipeKey, validateErr)
					}
					if _, exists := templatesByType[document.RecipeTypeID][recipe.TemplateID]; !exists {
						t.Fatalf("recipe %s references missing template %s", recipe.RecipeKey, recipe.TemplateID)
					}
				}
			}
			if templateFiles == 0 || recipeFiles == 0 || recipeRows == 0 {
				t.Fatalf("package contains no importable JEI v5 data: templates=%d recipe files=%d recipes=%d", templateFiles, recipeFiles, recipeRows)
			}
		})
	}
}
