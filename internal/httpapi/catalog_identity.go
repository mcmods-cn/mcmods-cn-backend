package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

type catalogIdentity struct {
	ID       string
	PublicID string
}

func newCatalogIdentity(entityType, canonicalKey string) catalogIdentity {
	digest := sha256.Sum256([]byte("mcmods-catalog/v1\x00" + entityType + "\x00" + strings.ToLower(strings.TrimSpace(canonicalKey))))
	encoded := hex.EncodeToString(digest[:])
	prefix := map[string]string{
		"resource":    "res",
		"recipe":      "rcp",
		"recipe_type": "rct",
		"tag":         "tag",
		"structure":   "str",
		"document":    "doc",
	}[entityType]
	if prefix == "" {
		prefix = "ent"
	}
	return catalogIdentity{ID: prefix + "_" + encoded[:32], PublicID: encoded[:24]}
}

func catalogSnapshotID(kind, revisionID, entityID, qualifier string) string {
	identity := newCatalogIdentity("document", kind+"\x00"+revisionID+"\x00"+entityID+"\x00"+qualifier)
	return "snp_" + strings.TrimPrefix(identity.ID, "doc_")
}

func resourceKindForRegistry(registry string) string {
	normalized := strings.ToLower(strings.TrimSpace(registry))
	switch normalized {
	case "items", "item":
		return "minecraft.item"
	case "blocks", "block":
		return "minecraft.block"
	case "fluids", "fluid":
		return "minecraft.fluid"
	case "entity_types", "entities", "entity_type":
		return "minecraft.entity_type"
	case "mob_effects", "effects", "mob_effect":
		return "minecraft.mob_effect"
	case "enchantments", "enchantment":
		return "minecraft.enchantment"
	case "biomes", "biome":
		return "minecraft.biome"
	case "dimensions", "dimension":
		return "minecraft.dimension"
	case "key_mappings", "key_mapping":
		return "minecraft.key_mapping"
	case "advancements", "advancement":
		return "minecraft.advancement"
	case "loot_tables", "loot_table":
		return "minecraft.loot_table"
	case "structures", "world_structures", "structure":
		return "minecraft.structure"
	default:
		return "import.document"
	}
}

func resourceKindForIngredient(ingredientKind, ingredientType string) string {
	value := strings.ToLower(strings.TrimSpace(ingredientKind + " " + ingredientType))
	switch {
	case strings.Contains(value, "slurry"):
		return "mekanism.slurry"
	case strings.Contains(value, "pigment"):
		return "mekanism.pigment"
	case strings.Contains(value, "infuse"), strings.Contains(value, "infusion"):
		return "mekanism.infusion"
	case strings.Contains(value, "gas"), strings.Contains(value, "chemical"):
		return "mekanism.gas"
	case strings.Contains(value, "fluid"):
		return "minecraft.fluid"
	case strings.Contains(value, "item"):
		return "minecraft.item"
	default:
		return "jei.ingredient"
	}
}

func resourceKindForDocument(kind string, data map[string]any) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "ingredients":
		return resourceKindForIngredient(exportString(data["ingredient_kind"]), exportString(data["ingredient_type"]))
	case "advancements":
		return "minecraft.advancement"
	case "key_mappings":
		return "minecraft.key_mapping"
	case "biomes":
		return "minecraft.biome"
	case "dimensions":
		return "minecraft.dimension"
	case "world_structures":
		return "minecraft.structure"
	case "loot_tables":
		return "minecraft.loot_table"
	default:
		return resourceKindForRegistry(kind)
	}
}

func resourceParts(canonicalID string) (string, string) {
	namespace, resourcePath, found := strings.Cut(strings.TrimSpace(canonicalID), ":")
	if !found {
		return "unknown", strings.TrimSpace(canonicalID)
	}
	return strings.ToLower(namespace), resourcePath
}

func resourceIdentity(kindCode, canonicalID string) catalogIdentity {
	return newCatalogIdentity("resource", kindCode+"\x00"+strings.TrimSpace(canonicalID))
}

func tagIdentity(registry, canonicalID string) catalogIdentity {
	return newCatalogIdentity("tag", strings.ToLower(strings.TrimSpace(registry))+"\x00"+strings.TrimSpace(canonicalID))
}

func recipeTypeIdentity(canonicalID string) catalogIdentity {
	return newCatalogIdentity("recipe_type", strings.TrimSpace(canonicalID))
}

func recipeIdentity(packageID, recipeTypeID, sourceID string, canonical bool) catalogIdentity {
	key := strings.TrimSpace(recipeTypeID) + "\x00"
	if canonical {
		key += "source\x00" + strings.TrimSpace(sourceID)
	} else {
		key += "package\x00" + strings.TrimSpace(packageID) + "\x00" + strings.TrimSpace(sourceID)
	}
	return newCatalogIdentity("recipe", key)
}
