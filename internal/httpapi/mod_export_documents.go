package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type exportDocumentEntry struct {
	ID             string
	Namespace      string
	TranslationKey string
	Names          map[string]any
	IconPath       string
	PreviewPath    string
	Data           map[string]any
}

// queueExportDocumentEntries materializes stable list projections while the
// source JSON is already in memory. Request handlers can then paginate normal
// rows instead of repeatedly expanding large JSON documents in PostgreSQL.
func queueExportDocumentEntries(batch *modExportWriteBatch, resolver catalogResourceIdentityResolver, revisions map[string]string, assetPath string, raw []byte) error {
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("decode document index %s: %w", assetPath, err)
	}
	if err := validateCatalogDocumentContract(assetPath, document); err != nil {
		return err
	}
	entries := exportDocumentEntries(assetPath, document)
	for _, entry := range entries {
		revisionID, revisionErr := exportRevisionForNamespace(revisions, entry.Namespace)
		if revisionErr != nil {
			return fmt.Errorf("document %s entry %q: %w", assetPath, entry.ID, revisionErr)
		}
		encodedNames, err := json.Marshal(supportedExportNames(entry.Names))
		if err != nil {
			return err
		}
		filterSupportedExportLocalizedFields(entry.Data)
		encodedData, err := json.Marshal(entry.Data)
		if err != nil {
			return err
		}
		encodedData = compactImportJSONObject(
			encodedData,
			"id", "namespace", "path", "translation_key", "names",
		)
		kind := exportDocumentKind(assetPath)
		kindCode := resourceKindForDocument(kind, entry.Data)
		identity := resolveExportResourceIdentity(resolver, kindCode, kind, entry.ID, entry.Namespace)
		namespace, resourcePath := identity.Namespace, identity.ResourcePath
		if entry.Namespace != "" {
			namespace = strings.ToLower(entry.Namespace)
		}
		queueCatalogResource(batch, catalogResourceImportRow{
			EntityID: identity.ID, PublicID: identity.PublicID, KindCode: kindCode, CanonicalID: identity.CanonicalID, RawID: identity.RawID,
			Namespace: namespace, ResourcePath: resourcePath, RevisionID: revisionID,
			SnapshotID: catalogSnapshotID("resource", revisionID, identity.ID, ""), Registry: kind,
			TranslationKey: entry.TranslationKey, Names: string(encodedNames), Data: string(encodedData),
			IconPath: entry.IconPath, PreviewPath: entry.PreviewPath,
		})
	}
	return nil
}

func exportRevisionForNamespace(revisions map[string]string, namespace string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(namespace))
	if normalized == "" {
		return "", errors.New("entry has no namespace")
	}
	if revisionID := strings.TrimSpace(revisions[normalized]); revisionID != "" {
		return revisionID, nil
	}
	return "", fmt.Errorf("namespace %q has no import revision", normalized)
}

func resolveExportResourceIdentity(
	resolver catalogResourceIdentityResolver,
	kindCode, sourceKind, objectID, explicitNamespace string,
) catalogResolvedResourceIdentity {
	if strings.EqualFold(strings.TrimSpace(sourceKind), "key_mappings") {
		namespace, resourcePath, valid := exportSourceResourceParts(sourceKind, objectID, explicitNamespace)
		if valid {
			identity := resolver.resolve(kindCode, namespace+":"+resourcePath)
			identity.RawID = objectID
			return identity
		}
	}
	return resolver.resolve(kindCode, objectID)
}

func exportSourceResourceParts(sourceKind, objectID, explicitNamespace string) (string, string, bool) {
	objectID = strings.TrimSpace(objectID)
	namespace := strings.ToLower(strings.TrimSpace(explicitNamespace))
	if strings.EqualFold(strings.TrimSpace(sourceKind), "key_mappings") {
		resourcePath := objectID
		if namespace == "" {
			var found bool
			namespace, resourcePath, found = strings.Cut(objectID, ".")
			if !found {
				return "", "", false
			}
		} else {
			resourcePath = strings.TrimPrefix(objectID, namespace+".")
		}
		return namespace, strings.TrimSpace(resourcePath), namespace != "" && strings.TrimSpace(resourcePath) != ""
	}
	parsedNamespace, resourcePath, found := strings.Cut(objectID, ":")
	if !found || strings.TrimSpace(parsedNamespace) == "" || strings.TrimSpace(resourcePath) == "" {
		return "", "", false
	}
	return strings.ToLower(strings.TrimSpace(parsedNamespace)), strings.TrimSpace(resourcePath), true
}

func validateCatalogDocumentContract(assetPath string, document map[string]any) error {
	schemaVersion := strings.TrimSpace(exportString(document["schema_version"]))
	exportMode := strings.TrimSpace(exportString(document["export_mode"]))
	switch assetPath {
	case "worldgen/natural_generation.json":
		if schemaVersion != "mcmods-natural-generation/v2" || exportMode != "normalized_catalog" {
			return fmt.Errorf("%s must use mcmods-natural-generation/v2 normalized_catalog", assetPath)
		}
	case "worldgen/structures.json":
		if schemaVersion != "mcmods-structures/v2" || exportMode != "catalog_only" {
			return fmt.Errorf("%s must use mcmods-structures/v2 catalog_only", assetPath)
		}
		if exported, exists := document["internal_templates_exported"]; exists && exported != false {
			return fmt.Errorf("%s must not export internal structure templates", assetPath)
		}
	}
	return nil
}

func exportDocumentKind(assetPath string) string {
	switch assetPath {
	case "advancements/advancements.json":
		return "advancements"
	case "registries/key_mappings.json":
		return "key_mappings"
	case "worldgen/biomes.json":
		return "biomes"
	case "worldgen/dimensions.json":
		return "dimensions"
	case "worldgen/natural_generation.json":
		return "natural_generation"
	case "worldgen/structures.json":
		return "world_structures"
	case "worldgen/loot_tables.json":
		return "loot_tables"
	case "ingredients/ingredients.json":
		return "ingredients"
	case "worldgen/data_files.json":
		return "worldgen_data"
	default:
		return ""
	}
}

func exportDocumentEntries(assetPath string, document map[string]any) []exportDocumentEntry {
	var values []map[string]any
	dimensionTypes := make(map[string]map[string]any)
	switch assetPath {
	case "advancements/advancements.json":
		values = exportObjectArray(document["advancements"])
	case "registries/key_mappings.json":
		values = exportObjectArray(document["entries"])
	case "worldgen/biomes.json":
		values = exportObjectArray(document["biomes"])
	case "worldgen/dimensions.json":
		values = exportObjectArray(document["dimensions"])
		for _, dimensionType := range exportObjectArray(document["dimension_types"]) {
			identifier := strings.TrimSpace(exportString(dimensionType["id"]))
			definition := exportObject(dimensionType["definition"])
			if len(definition) == 0 {
				definition = exportObject(dimensionType["runtime_definition"])
			}
			if identifier != "" && len(definition) > 0 {
				dimensionTypes[identifier] = definition
			}
		}
	case "worldgen/structures.json":
		values = exportObjectArray(document["structures"])
	case "worldgen/loot_tables.json":
		values = exportObjectArray(document["loot_tables"])
	case "worldgen/data_files.json":
		values = exportObjectArray(document["files"])
	case "worldgen/natural_generation.json":
		values = exportObjectArray(document["entries"])
	case "ingredients/ingredients.json":
		for _, ingredientType := range exportObjectArray(document["types"]) {
			for _, entry := range exportObjectArray(ingredientType["entries"]) {
				entry["ingredient_type"] = ingredientType["ingredient_type"]
				values = append(values, entry)
			}
		}
	default:
		return nil
	}

	result := make([]exportDocumentEntry, 0, len(values))
	for _, value := range values {
		entry := exportDocumentEntry{Data: value, Names: exportObject(value["names"])}
		entry.ID = strings.TrimSpace(exportString(value["id"]))
		entry.TranslationKey = strings.TrimSpace(exportString(value["translation_key"]))
		if assetPath == "worldgen/natural_generation.json" {
			entry.ID = strings.TrimSpace(exportString(value["generation_id"]))
		}
		if assetPath == "worldgen/structures.json" {
			entry.ID = strings.TrimSpace(exportString(value["structure_id"]))
		}
		if assetPath == "ingredients/ingredients.json" && entry.ID == "" {
			entry.ID = strings.TrimSpace(exportString(value["resource_location"]))
			if entry.ID == "" {
				entry.ID = strings.TrimSpace(exportString(value["unique_id"]))
			}
		}
		if assetPath == "worldgen/data_files.json" && entry.ID == "" {
			entry.ID = strings.TrimSpace(exportString(value["path"]))
		}
		if assetPath == "advancements/advancements.json" {
			display := exportObject(value["display"])
			entry.Names = exportObject(display["title_names"])
			entry.TranslationKey = strings.TrimSpace(exportString(display["title_translation_key"]))
			// The complete advancement document is retained as a text asset and
			// title names are normalized into resource_import_snapshots.names.
			// Avoid sending the same large locale map twice to remote PostgreSQL.
			delete(display, "title_names")
			itemID := exportString(exportObject(display["icon"])["item"])
			entry.IconPath = exportItemIconPath(itemID, 32)
			entry.PreviewPath = exportItemIconPath(itemID, 256)
		}
		if assetPath == "ingredients/ingredients.json" {
			icons := exportObject(value["icons"])
			entry.IconPath = exportString(icons["32"])
			entry.PreviewPath = exportString(icons["256"])
		}
		if assetPath == "worldgen/loot_tables.json" {
			entry.Data["category"] = normalizeLootTableCategory(
				entry.ID,
				exportString(entry.Data["path"]),
				exportString(entry.Data["category"]),
			)
		}
		if assetPath == "worldgen/structures.json" {
			biomeTag, biomeIDs := exportBiomeSelectorReferences(value["biomes"])
			if biomeTag != "" {
				entry.Data["biome_tag"] = biomeTag
			}
			if len(biomeIDs) > 0 {
				entry.Data["biome_ids"] = biomeIDs
			}
		}
		if assetPath == "worldgen/dimensions.json" {
			runtimeDefinition := exportObject(value["runtime_definition"])
			if len(runtimeDefinition) == 0 {
				runtimeDefinition = exportObject(value["definition"])
			}
			typeID := strings.TrimSpace(exportString(runtimeDefinition["type"]))
			if dimensionType := dimensionTypes[typeID]; len(dimensionType) > 0 {
				entry.Data["dimension_type_definition"] = dimensionType
			}
			if biomeIDs := exportDimensionBiomeIDs(runtimeDefinition); len(biomeIDs) > 0 {
				entry.Data["biome_ids"] = biomeIDs
			}
		}
		if assetPath == "worldgen/biomes.json" {
			runtimeDefinition := exportObject(value["runtime_definition"])
			if len(runtimeDefinition) == 0 {
				runtimeDefinition = exportObject(value["definition"])
			}
			spawners := exportObject(runtimeDefinition["spawners"])
			if entityIDs := exportReferencedIDs(spawners, "type"); len(entityIDs) > 0 {
				entry.Data["spawned_entity_ids"] = entityIDs
			}
			if featureIDs := exportStringLeaves(runtimeDefinition["features"]); len(featureIDs) > 0 {
				entry.Data["feature_ids"] = featureIDs
			}
			if carverIDs := exportStringLeaves(runtimeDefinition["carvers"]); len(carverIDs) > 0 {
				entry.Data["carver_ids"] = carverIDs
			}
		}
		entry.Namespace = strings.TrimSpace(exportString(value["namespace"]))
		if entry.Namespace == "" {
			separator := ":"
			if assetPath == "registries/key_mappings.json" {
				separator = "."
			}
			entry.Namespace, _, _ = strings.Cut(entry.ID, separator)
		}
		if entry.ID != "" {
			result = append(result, entry)
		}
	}
	return result
}

func exportReferencedIDs(value any, field string) []any {
	result := make([]any, 0)
	seen := make(map[string]struct{})
	var visit func(any)
	visit = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, nested := range typed {
				if strings.EqualFold(strings.TrimSpace(key), field) {
					for _, identifier := range exportStringLeaves(nested) {
						identifier = strings.TrimPrefix(strings.TrimSpace(identifier), "#")
						if identifier == "" {
							continue
						}
						if _, exists := seen[identifier]; !exists {
							seen[identifier] = struct{}{}
							result = append(result, identifier)
						}
					}
				}
				visit(nested)
			}
		case []any:
			for _, nested := range typed {
				visit(nested)
			}
		}
	}
	visit(value)
	return result
}

func exportDimensionBiomeIDs(definition map[string]any) []any {
	result := exportReferencedIDs(definition, "biome")
	seen := make(map[string]struct{}, len(result)+8)
	for _, value := range result {
		if identifier, ok := value.(string); ok {
			seen[identifier] = struct{}{}
		}
	}
	generator := exportObject(definition["generator"])
	biomeSource := exportObject(generator["biome_source"])
	typeID := strings.ToLower(strings.TrimSpace(exportString(biomeSource["type"])))
	presetID := strings.ToLower(strings.TrimSpace(exportString(biomeSource["preset"])))
	known := []string{}
	switch {
	case typeID == "minecraft:the_end":
		known = []string{"minecraft:the_end", "minecraft:small_end_islands", "minecraft:end_midlands", "minecraft:end_highlands", "minecraft:end_barrens"}
	case presetID == "minecraft:nether":
		known = []string{"minecraft:nether_wastes", "minecraft:soul_sand_valley", "minecraft:crimson_forest", "minecraft:warped_forest", "minecraft:basalt_deltas"}
	}
	for _, identifier := range known {
		if _, exists := seen[identifier]; exists {
			continue
		}
		seen[identifier] = struct{}{}
		result = append(result, identifier)
	}
	return result
}

func exportStringLeaves(value any) []string {
	result := make([]string, 0)
	seen := make(map[string]struct{})
	var visit func(any)
	visit = func(current any) {
		switch typed := current.(type) {
		case string:
			identifier := strings.TrimSpace(typed)
			if identifier != "" {
				if _, exists := seen[identifier]; !exists {
					seen[identifier] = struct{}{}
					result = append(result, identifier)
				}
			}
		case []any:
			for _, nested := range typed {
				visit(nested)
			}
		case map[string]any:
			for _, nested := range typed {
				visit(nested)
			}
		}
	}
	visit(value)
	return result
}

func exportBiomeSelectorReferences(value any) (string, []any) {
	values := exportStringLeaves(value)
	result := make([]any, 0, len(values))
	biomeTag := ""
	for _, identifier := range values {
		identifier = strings.TrimSpace(identifier)
		if strings.HasPrefix(identifier, "#") {
			if biomeTag == "" {
				biomeTag = identifier
			}
			continue
		}
		if identifier != "" {
			result = append(result, identifier)
		}
	}
	return biomeTag, result
}

func normalizeLootTableCategory(id, path, category string) string {
	category = strings.ToLower(strings.TrimSpace(category))
	path = strings.ToLower(strings.TrimSpace(path))
	if path == "" {
		_, path, _ = strings.Cut(strings.ToLower(strings.TrimSpace(id)), ":")
	}
	switch {
	case category == "block" || category == "blocks" || strings.HasPrefix(path, "blocks/"):
		return "blocks"
	case category == "chest" || category == "chests" || strings.HasPrefix(path, "chests/"):
		return "chests"
	case category == "entity" || category == "entities" || strings.HasPrefix(path, "entities/"):
		return "entities"
	case category == "fishing" || strings.HasPrefix(path, "gameplay/fishing") || strings.HasPrefix(path, "fishing/"):
		return "fishing"
	case category == "archaeology" || strings.HasPrefix(path, "archaeology/"):
		return "archaeology"
	case category == "equipment" || strings.HasPrefix(path, "equipment/"):
		return "equipment"
	case category == "gameplay" || strings.HasPrefix(path, "gameplay/"):
		return "gameplay"
	default:
		return "other"
	}
}

func exportObjectArray(value any) []map[string]any {
	values, _ := value.([]any)
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if object, ok := value.(map[string]any); ok {
			result = append(result, object)
		}
	}
	return result
}

func exportObject(value any) map[string]any {
	object, _ := value.(map[string]any)
	if object == nil {
		return map[string]any{}
	}
	return object
}

func exportItemIconPath(itemID string, size int) string {
	namespace, objectPath, found := strings.Cut(strings.TrimSpace(itemID), ":")
	if !found || namespace == "" || objectPath == "" {
		return ""
	}
	return fmt.Sprintf("icons/items/%d/%s/%s.png", size, namespace, objectPath)
}
