package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
)

type exportDocumentEntry struct {
	ID          string
	Namespace   string
	Names       map[string]any
	IconPath    string
	PreviewPath string
	Data        map[string]any
}

// queueExportDocumentEntries materializes stable list projections while the
// source JSON is already in memory. Request handlers can then paginate normal
// rows instead of repeatedly expanding large JSON documents in PostgreSQL.
func queueExportDocumentEntries(batch *modExportWriteBatch, revisionID, assetPath string, raw []byte) error {
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("decode document index %s: %w", assetPath, err)
	}
	entries := exportDocumentEntries(assetPath, document)
	for ordinal, entry := range entries {
		encodedNames, err := json.Marshal(entry.Names)
		if err != nil {
			return err
		}
		encodedData, err := json.Marshal(entry.Data)
		if err != nil {
			return err
		}
		kind := exportDocumentKind(assetPath)
		kindCode := resourceKindForDocument(kind, entry.Data)
		identity := resourceIdentity(kindCode, entry.ID)
		namespace, resourcePath := resourceParts(entry.ID)
		if entry.Namespace != "" {
			namespace = strings.ToLower(entry.Namespace)
		}
		queueCatalogResource(batch, catalogResourceImportRow{
			EntityID: identity.ID, PublicID: identity.PublicID, KindCode: kindCode, CanonicalID: entry.ID,
			Namespace: namespace, ResourcePath: resourcePath, RevisionID: revisionID,
			SnapshotID: catalogSnapshotID("resource", revisionID, identity.ID, ""), Registry: kind,
			Names: string(encodedNames), Data: string(encodedData), IconPath: entry.IconPath, PreviewPath: entry.PreviewPath,
		})
		_ = ordinal
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
	switch assetPath {
	case "advancements/advancements.json":
		values = exportObjectArray(document["advancements"])
	case "registries/key_mappings.json":
		values = exportObjectArray(document["entries"])
	case "worldgen/biomes.json":
		values = exportObjectArray(document["biomes"])
	case "worldgen/dimensions.json":
		values = exportObjectArray(document["dimensions"])
	case "worldgen/structures.json":
		values = exportObjectArray(document["structures"])
	case "worldgen/loot_tables.json":
		values = exportObjectArray(document["loot_tables"])
	case "worldgen/data_files.json":
		values = exportObjectArray(document["files"])
	case "worldgen/natural_generation.json":
		for _, category := range exportObjectArray(document["categories"]) {
			for _, entry := range exportObjectArray(category["entries"]) {
				entry["category"] = category["category"]
				entry["registry"] = category["registry"]
				values = append(values, entry)
			}
		}
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
			itemID := exportString(exportObject(display["icon"])["item"])
			entry.IconPath = exportItemIconPath(itemID, 32)
			entry.PreviewPath = exportItemIconPath(itemID, 256)
		}
		if assetPath == "ingredients/ingredients.json" {
			icons := exportObject(value["icons"])
			entry.IconPath = exportString(icons["32"])
			entry.PreviewPath = exportString(icons["256"])
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
