package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// A category's technical namespace is not the recipe author's namespace. The
// scope is built only from the package namespaces already admitted by the Mod
// identity/MC/loader/version/hash gates and explicit recipe provenance.
// Category and template observations may be dependencies of those recipes;
// resource, tag and document namespace attribution stays strict and unchanged.
type exportRecipeImportScope struct {
	revisions  map[string]string
	document   exportJEICategoryDocument
	categories map[string]exportJEICategory
	owners     map[string]map[string]struct{}
	indexed    map[exportScopedRecipeKey]exportJEIRecipeIndexRow
}
type exportScopedRecipeKey struct{ RecipeType, RecipeKey string }

func scopedRecipeKey(recipeType, recipeID, key string) exportScopedRecipeKey {
	if strings.TrimSpace(key) == "" {
		key = recipeID
	}
	return exportScopedRecipeKey{strings.TrimSpace(recipeType), strings.TrimSpace(key)}
}

func exportRecipeOwnerNamespace(revisions map[string]string, recipeType, recipeID, modID, source string, canonical *bool) (string, error) {
	var expected string
	switch strings.TrimSpace(source) {
	case "recipe_id_namespace":
		if canonical == nil || !*canonical {
			return "", fmt.Errorf("recipe-ID provenance requires a canonical recipe ID")
		}
		expected = exportResourceNamespace(strings.TrimSpace(recipeID))
	case "recipe_type_namespace":
		if canonical == nil || *canonical {
			return "", fmt.Errorf("category provenance requires a noncanonical generated recipe ID")
		}
		expected = exportResourceNamespace(strings.TrimSpace(recipeType))
	default:
		return "", fmt.Errorf("unsupported recipe source provenance %q", source)
	}
	if expected == "" || strings.ToLower(strings.TrimSpace(modID)) != expected {
		return "", fmt.Errorf("recipe source Mod ID %q disagrees with %s namespace %q", modID, source, expected)
	}
	if _, err := exportRevisionForNamespace(revisions, expected); err != nil {
		return "", fmt.Errorf("recipe owner: %w", err)
	}
	return expected, nil
}

func newExportRecipeImportScope(revisions map[string]string, raw []byte) (*exportRecipeImportScope, error) {
	scope := &exportRecipeImportScope{revisions: revisions, categories: map[string]exportJEICategory{}, owners: map[string]map[string]struct{}{}, indexed: map[exportScopedRecipeKey]exportJEIRecipeIndexRow{}}
	if err := json.Unmarshal(raw, &scope.document); err != nil {
		return nil, fmt.Errorf("decode recipe ownership index: %w", err)
	}
	if scope.document.SchemaVersion != "mcmods-jei-categories/v6" && scope.document.SchemaVersion != "mcmods-jei-categories/v7" {
		return nil, fmt.Errorf("unsupported JEI category schema: %s", scope.document.SchemaVersion)
	}
	for _, category := range scope.document.Categories {
		id := strings.TrimSpace(category.RecipeTypeID)
		if id == "" {
			return nil, fmt.Errorf("JEI category has no recipe_type_id")
		}
		if _, exists := scope.categories[id]; exists {
			return nil, fmt.Errorf("duplicate JEI category %s", id)
		}
		scope.categories[id] = category
		scope.owners[id] = map[string]struct{}{}
	}
	for _, index := range scope.document.Recipes {
		category, exists := scope.categories[strings.TrimSpace(index.RecipeTypeID)]
		if !exists {
			return nil, fmt.Errorf("recipe index refers to an undeclared category %q", index.RecipeTypeID)
		}
		if err := validateExportRecipeSource(index.OriginKind, index.SourceModID, index.SourceModVersion, index.SourceModIDSource); err != nil {
			return nil, err
		}
		canonical, err := validateExportRecipeIdentity(index.RecipeID, index.RecipeIDSource, index.RecipeIDCanonical)
		if err != nil {
			return nil, err
		}
		if (canonical && index.OriginKind != "registered_recipe") || (!canonical && index.OriginKind != "jei_generated_recipe") {
			return nil, fmt.Errorf("recipe index origin disagrees with its canonical identity")
		}
		owner, err := exportRecipeOwnerNamespace(revisions, index.RecipeTypeID, index.RecipeID, index.SourceModID, index.SourceModIDSource, index.RecipeIDCanonical)
		if err != nil {
			return nil, err
		}
		if index.RecipeCollection != "" && index.RecipeCollection != category.RecipeCollection {
			return nil, fmt.Errorf("recipe index collection disagrees with category %q", index.RecipeTypeID)
		}
		key := scopedRecipeKey(index.RecipeTypeID, index.RecipeID, index.RecipeKey)
		if _, exists := scope.indexed[key]; exists {
			return nil, fmt.Errorf("duplicate scoped recipe index %q", key.RecipeKey)
		}
		scope.indexed[key] = index
		scope.owners[strings.TrimSpace(index.RecipeTypeID)][owner] = struct{}{}
	}
	for id, category := range scope.categories {
		if len(scope.owners[id]) > 0 {
			continue
		}
		namespace := exportResourceNamespace(id)
		if _, err := exportRevisionForNamespace(revisions, namespace); err == nil {
			scope.owners[id][namespace] = struct{}{}
			continue
		}
		// v7 explicitly supports template-only observations without recipe bodies.
		// They are dependencies of the admitted package target scopes, not evidence
		// that their category namespace grants another project's import authority.
		if scope.document.SchemaVersion == "mcmods-jei-categories/v7" && category.ExportMode == "templates_only" && category.RecipesOmitted && category.SourceRecipeCount > 0 && category.RecipeCount == 0 && category.ExportedRecipeCount == 0 {
			for owner, revision := range revisions {
				if strings.TrimSpace(revision) != "" {
					scope.owners[id][owner] = struct{}{}
				}
			}
			continue
		}
		return nil, fmt.Errorf("JEI category %q has no recipe owner in the admitted package namespaces", id)
	}
	return scope, nil
}

func (scope *exportRecipeImportScope) dependencyAliases(recipeType string) ([]map[string]string, error) {
	owners, exists := scope.owners[strings.TrimSpace(recipeType)]
	if !exists || len(owners) == 0 {
		return nil, fmt.Errorf("unscoped recipe category %q", recipeType)
	}
	namespaces := make([]string, 0, len(owners))
	for owner := range owners {
		namespaces = append(namespaces, owner)
	}
	sort.Strings(namespaces)
	result := make([]map[string]string, 0, len(namespaces))
	for _, owner := range namespaces {
		revision, err := exportRevisionForNamespace(scope.revisions, owner)
		if err != nil {
			return nil, err
		}
		result = append(result, map[string]string{exportResourceNamespace(recipeType): revision})
	}
	return result, nil
}

func (scope *exportRecipeImportScope) importTypes(ctx context.Context, tx pgx.Tx, packageID string) error {
	owners := make([]string, 0, len(scope.revisions))
	for owner := range scope.revisions {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		document := exportJEICategoryDocument{SchemaVersion: scope.document.SchemaVersion}
		aliases := map[string]string{}
		for _, category := range scope.document.Categories {
			if _, included := scope.owners[strings.TrimSpace(category.RecipeTypeID)][owner]; included {
				document.Categories = append(document.Categories, category)
				aliases[exportResourceNamespace(category.RecipeTypeID)] = scope.revisions[owner]
			}
		}
		if len(document.Categories) == 0 {
			continue
		}
		for _, index := range scope.document.Recipes {
			namespace, err := exportRecipeOwnerNamespace(scope.revisions, index.RecipeTypeID, index.RecipeID, index.SourceModID, index.SourceModIDSource, index.RecipeIDCanonical)
			if err != nil {
				return err
			}
			if namespace == owner {
				document.Recipes = append(document.Recipes, index)
			}
		}
		raw, err := json.Marshal(document)
		if err != nil {
			return err
		}
		if err = importExportRecipeTypes(ctx, tx, packageID, aliases, raw); err != nil {
			return err
		}
	}
	return nil
}

func (scope *exportRecipeImportScope) queueTemplates(batch *modExportWriteBatch, name string, document exportJEITemplateCollection) error {
	category, exists := scope.categories[strings.TrimSpace(document.RecipeTypeID)]
	if !exists || category.TemplateCollection != name {
		return fmt.Errorf("template collection is outside its declared scoped category")
	}
	if document.TemplateCount != category.TemplateCount {
		return fmt.Errorf("template collection count disagrees with its scoped category")
	}
	aliases, err := scope.dependencyAliases(document.RecipeTypeID)
	if err != nil {
		return err
	}
	for _, revisions := range aliases {
		if err = queueExportJEITemplateCollection(batch, revisions, name, document); err != nil {
			return err
		}
	}
	return nil
}

func (scope *exportRecipeImportScope) queueRecipes(batch *modExportWriteBatch, resolver catalogResourceIdentityResolver, packageID, name string, document exportJEIRecipeCollection) error {
	category, exists := scope.categories[strings.TrimSpace(document.RecipeTypeID)]
	if !exists || category.RecipeCollection != name || category.TemplateCollection != document.TemplateCollection {
		return fmt.Errorf("recipe collection is outside its declared scoped category")
	}
	for _, binding := range document.Recipes {
		key := scopedRecipeKey(document.RecipeTypeID, binding.RecipeID, binding.RecipeKey)
		index, exists := scope.indexed[key]
		if !exists || !index.LayoutAvailable {
			return fmt.Errorf("recipe binding %q has no matching layout index", key.RecipeKey)
		}
		if binding.RecipeID != index.RecipeID || binding.RecipeIDSource != index.RecipeIDSource || binding.OriginKind != index.OriginKind ||
			binding.SourceModID != index.SourceModID || binding.SourceModVersion != index.SourceModVersion || binding.SourceModIDSource != index.SourceModIDSource ||
			binding.RecipeIDCanonical == nil || index.RecipeIDCanonical == nil || *binding.RecipeIDCanonical != *index.RecipeIDCanonical || binding.TemplateID != index.TemplateID {
			return fmt.Errorf("recipe binding %q disagrees with its indexed source owner", key.RecipeKey)
		}
		owner, err := exportRecipeOwnerNamespace(scope.revisions, document.RecipeTypeID, binding.RecipeID, binding.SourceModID, binding.SourceModIDSource, binding.RecipeIDCanonical)
		if err != nil {
			return err
		}
		revision, err := exportRevisionForNamespace(scope.revisions, owner)
		if err != nil {
			return err
		}
		single := document
		single.Recipes = []exportJEIRecipeBinding{binding}
		single.Count = 1
		if err = queueExportJEIRecipeCollection(batch, resolver, packageID, map[string]string{exportResourceNamespace(document.RecipeTypeID): revision}, name, single); err != nil {
			return err
		}
	}
	return nil
}
