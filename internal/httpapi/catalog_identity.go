package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/jackc/pgx/v5"
)

type catalogIdentity struct {
	ID       string
	PublicID string
}

type catalogResourceNamespaceOwner struct {
	ProjectCode       string
	PrimaryIdentifier string
}

type catalogResourceIdentityResolver map[string]catalogResourceNamespaceOwner

type catalogResolvedResourceIdentity struct {
	catalogIdentity
	CanonicalID  string
	Namespace    string
	ResourcePath string
	RawID        string
}

type catalogResourceResolverQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadCatalogResourceIdentityResolver(ctx context.Context, query catalogResourceResolverQuery) (catalogResourceIdentityResolver, error) {
	rows, err := query.Query(ctx, `select lower(identifier.identifier),mod.project_code,primary_identifier.identifier
		from mod_identifiers identifier
		join mods mod on mod.id=identifier.mod_id
		join mod_identifiers primary_identifier on primary_identifier.mod_id=mod.id and primary_identifier.is_primary`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	resolver := catalogResourceIdentityResolver{}
	for rows.Next() {
		var namespace string
		var owner catalogResourceNamespaceOwner
		if err = rows.Scan(&namespace, &owner.ProjectCode, &owner.PrimaryIdentifier); err != nil {
			return nil, err
		}
		resolver[namespace] = owner
	}
	return resolver, rows.Err()
}

func (resolver catalogResourceIdentityResolver) resolve(kindCode, rawID string) catalogResolvedResourceIdentity {
	rawID = strings.TrimSpace(rawID)
	namespace, resourcePath := resourceParts(rawID)
	owner, aliased := resolver[strings.ToLower(namespace)]
	if !aliased || strings.TrimSpace(resourcePath) == "" {
		identity := resourceIdentity(kindCode, rawID)
		return catalogResolvedResourceIdentity{catalogIdentity: identity, CanonicalID: rawID, Namespace: namespace, ResourcePath: resourcePath, RawID: rawID}
	}
	canonicalID := owner.PrimaryIdentifier + ":" + resourcePath
	identity := newCatalogIdentity("resource", kindCode+"\x00mod\x00"+owner.ProjectCode+"\x00"+resourcePath)
	return catalogResolvedResourceIdentity{
		catalogIdentity: identity, CanonicalID: canonicalID, Namespace: strings.ToLower(owner.PrimaryIdentifier),
		ResourcePath: resourcePath, RawID: rawID,
	}
}

func newCatalogIdentity(entityType, canonicalKey string) catalogIdentity {
	identity := catalogIdentityKey(entityType, canonicalKey)
	identity.PublicID = randomCatalogPublicID()
	return identity
}

func catalogIdentityKey(entityType, canonicalKey string) catalogIdentity {
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
	return catalogIdentity{ID: prefix + "_" + encoded[:32]}
}

func randomCatalogPublicID() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	result := make([]byte, 9)
	entropy := make([]byte, 16)
	position := 0
	for position < len(result) {
		if _, err := rand.Read(entropy); err != nil {
			panic("generate catalog public ID: operating system CSPRNG unavailable")
		}
		for _, value := range entropy {
			if value >= 248 {
				continue
			}
			result[position] = alphabet[int(value)%len(alphabet)]
			position++
			if position == len(result) {
				break
			}
		}
	}
	return string(result)
}

func catalogSnapshotID(kind, revisionID, entityID, qualifier string) string {
	identity := catalogIdentityKey("document", kind+"\x00"+revisionID+"\x00"+entityID+"\x00"+qualifier)
	return "snp_" + strings.TrimPrefix(identity.ID, "doc_")
}

func resourceKindForRegistry(registry string) string {
	normalized := strings.ToLower(strings.TrimSpace(registry))
	// Tag exports use registry keys such as minecraft:item, while registry
	// snapshots use their short names. They must resolve to the same identity.
	normalized = strings.TrimPrefix(normalized, "minecraft:")
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
	case "game_settings", "game_setting", "game_rules", "game_rule":
		return "minecraft.game_setting"
	case "structures", "world_structures", "structure":
		return "minecraft.structure"
	default:
		// Unknown registries must remain separate resource identities. Keeping a
		// bounded digest in the kind code avoids exposing attacker-controlled
		// registry text as a schema key while preserving stable case-insensitive
		// identity across repeated imports.
		digest := sha256.Sum256([]byte(normalized))
		return "import.document." + hex.EncodeToString(digest[:12])
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
	case "natural_generation":
		return "minecraft.natural_generation"
	case "loot_tables":
		return "minecraft.loot_table"
	case "game_settings":
		return "minecraft.game_setting"
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
