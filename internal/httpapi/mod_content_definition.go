package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5"
)

const modContentDefinitionSchemaVersion = 1

type modContentTemplateRows interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func normalizeModContentEntryDefinition(
	ctx context.Context,
	query modContentImageQuerier,
	modID, versionID int64,
	kindCode string,
	sectionPublicID *string,
	entryTypeCode string,
	definition map[string]any,
	useImportAliases bool,
) (map[string]any, error) {
	if sectionPublicID == nil || strings.TrimSpace(*sectionPublicID) == "" {
		if strings.EqualFold(strings.TrimSpace(entryTypeCode), "default") {
			return map[string]any{}, nil
		}
		return nil, errCatalogEditorReference
	}
	var raw []byte
	if err := query.QueryRow(ctx, `select template.definition
		from mod_content_sections section
		join mod_content_templates template on template.id=section.template_id
		where section.public_id=$1 and section.mod_id=$2 and section.version_id=$3
		  and section.status='active' and template.status='active'`,
		*sectionPublicID, modID, versionID).Scan(&raw); err != nil {
		return nil, err
	}
	var template modContentTemplateDefinition
	if err := json.Unmarshal(raw, &template); err != nil {
		return nil, err
	}
	entryType, err := selectModContentEntryType(template.EntryTypes, entryTypeCode, kindCode, false)
	if err != nil {
		return nil, err
	}
	return canonicalModContentDefinition(*entryType, definition, useImportAliases)
}

func selectModContentEntryType(entryTypes []modContentEntryTypeDefinition, entryTypeCode, kindCode string, enforceKind bool) (*modContentEntryTypeDefinition, error) {
	if len(entryTypes) == 0 && strings.EqualFold(strings.TrimSpace(entryTypeCode), "default") {
		return &modContentEntryTypeDefinition{Code: "default"}, nil
	}
	for index := range entryTypes {
		entryType := &entryTypes[index]
		if !strings.EqualFold(strings.TrimSpace(entryType.Code), strings.TrimSpace(entryTypeCode)) {
			continue
		}
		if !enforceKind || len(entryType.KindCodes) == 0 {
			return entryType, nil
		}
		for _, candidate := range entryType.KindCodes {
			if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(kindCode)) {
				return entryType, nil
			}
		}
		return nil, errCatalogEditorInvalid
	}
	return nil, errCatalogEditorReference
}

func canonicalModContentDefinition(entryType modContentEntryTypeDefinition, source map[string]any, useImportAliases bool) (map[string]any, error) {
	result := make(map[string]any)
	for _, group := range entryType.Groups {
		for _, field := range group.Fields {
			paths := [][]string{{field.Code}}
			if useImportAliases {
				// Exporter documents may use a canonical field code for a different
				// shape. For example, items expose durability as an object while the
				// website's tool durability is a number sourced from max_damage.
				// Import aliases therefore take precedence; manual edits still use
				// field.code exclusively.
				paths = append(append(make([][]string, 0, len(field.Paths)+1), field.Paths...), []string{field.Code})
			}
			value, exists := modContentDefinitionValue(source, paths)
			if !exists || value == nil {
				continue
			}
			normalized, keep, err := normalizeModContentFieldValue(field, value)
			if err != nil {
				return nil, fmt.Errorf("normalize field %s: %w", field.Code, errCatalogEditorInvalid)
			}
			if keep {
				result[field.Code] = normalized
			}
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 512*1024 || catalogJSONDepth(result, 0) > 20 {
		return nil, errCatalogEditorInvalid
	}
	return result, nil
}

func normalizeModContentFieldValue(field modContentEntryTypeField, value any) (any, bool, error) {
	switch field.Type {
	case "number":
		number, ok := catalogFiniteNumber(value)
		if !ok {
			return nil, false, errCatalogEditorInvalid
		}
		return number, true, nil
	case "text", "reference":
		text, ok := value.(string)
		if !ok || len(text) > 4096 {
			return nil, false, errCatalogEditorInvalid
		}
		text = strings.TrimSpace(text)
		return text, text != "", nil
	case "boolean":
		boolean, ok := value.(bool)
		if !ok {
			return nil, false, errCatalogEditorInvalid
		}
		return boolean, true, nil
	case "list", "reference-list":
		items, ok := value.([]any)
		if !ok {
			if text, valid := value.(string); valid {
				items = []any{text}
				ok = true
			}
		}
		if !ok || len(items) > 2048 {
			return nil, false, errCatalogEditorInvalid
		}
		result := make([]string, 0, len(items))
		seen := make(map[string]struct{}, len(items))
		for _, item := range items {
			text, ok := item.(string)
			text = strings.TrimSpace(text)
			if !ok || text == "" || len(text) > 512 {
				return nil, false, errCatalogEditorInvalid
			}
			if _, exists := seen[text]; exists {
				continue
			}
			seen[text] = struct{}{}
			result = append(result, text)
		}
		return result, len(result) > 0, nil
	case "json":
		encoded, err := json.Marshal(value)
		if err != nil || len(encoded) > 256*1024 || catalogJSONDepth(value, 0) > 16 {
			return nil, false, errCatalogEditorInvalid
		}
		var normalized any
		if err = json.Unmarshal(encoded, &normalized); err != nil {
			return nil, false, errCatalogEditorInvalid
		}
		return normalized, string(encoded) != "null" && string(encoded) != "{}" && string(encoded) != "[]", nil
	default:
		return nil, false, errCatalogEditorInvalid
	}
}

func canonicalizeCatalogResourceRows(ctx context.Context, tx pgx.Tx, rows []catalogResourceImportRow) error {
	templates, err := loadBuiltinModContentTemplates(ctx, tx)
	if err != nil {
		return err
	}
	dimensionBiomes, biomeDimensions := importedDimensionBiomeRelations(rows)
	for index := range rows {
		row := &rows[index]
		var source map[string]any
		if err = json.Unmarshal([]byte(nonEmptyJSONObject(row.Data)), &source); err != nil {
			return fmt.Errorf("decode %s definition: %w", row.CanonicalID, err)
		}
		if strings.EqualFold(row.KindCode, "minecraft.dimension") {
			source["biome_ids"] = mergeImportedReferenceLists(source["biome_ids"], dimensionBiomes[strings.ToLower(row.CanonicalID)])
		}
		if strings.EqualFold(row.KindCode, "minecraft.biome") {
			source["dimension_ids"] = mergeImportedReferenceLists(source["dimension_ids"], biomeDimensions[strings.ToLower(row.CanonicalID)])
		}
		entryTypeCode, canonical, normalizeErr := canonicalResourceDefinitionFromTemplates(templates, row.KindCode, row.ResourcePath, source)
		if normalizeErr != nil {
			return fmt.Errorf("normalize %s as %s: %w", row.CanonicalID, entryTypeCode, normalizeErr)
		}
		inferredEntryTypeCode := inferImportedEntryTypeCode(row.KindCode, row.ResourcePath, source)
		if entryTypeCode != inferredEntryTypeCode {
			log.Printf("resource document %s downgraded from inferred type %s to %s", row.CanonicalID, inferredEntryTypeCode, entryTypeCode)
		}
		encoded, _ := json.Marshal(canonical)
		row.EntryTypeCode = entryTypeCode
		row.DefinitionSchemaVersion = modContentDefinitionSchemaVersion
		row.Data = string(encoded)
	}
	return nil
}

func importedDimensionBiomeRelations(rows []catalogResourceImportRow) (map[string][]string, map[string][]string) {
	dimensionBiomes := make(map[string][]string)
	biomeDimensions := make(map[string][]string)
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.KindCode), "minecraft.dimension") {
			var source map[string]any
			if json.Unmarshal([]byte(nonEmptyJSONObject(row.Data)), &source) == nil {
				biomes := importedStringList(source["biome_ids"])
				dimensionID := strings.ToLower(strings.TrimSpace(row.CanonicalID))
				dimensionBiomes[dimensionID] = append(dimensionBiomes[dimensionID], biomes...)
				for _, biomeID := range biomes {
					key := strings.ToLower(biomeID)
					biomeDimensions[key] = append(biomeDimensions[key], row.CanonicalID)
				}
			}
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(row.KindCode), "minecraft.natural_generation") {
			continue
		}
		var source map[string]any
		if json.Unmarshal([]byte(nonEmptyJSONObject(row.Data)), &source) != nil {
			continue
		}
		dimensions := importedStringList(source["dimension_ids"])
		biomes := importedStringList(source["resolved_biome_ids"])
		for _, dimensionID := range dimensions {
			key := strings.ToLower(dimensionID)
			dimensionBiomes[key] = append(dimensionBiomes[key], biomes...)
		}
		for _, biomeID := range biomes {
			key := strings.ToLower(biomeID)
			biomeDimensions[key] = append(biomeDimensions[key], dimensions...)
		}
	}
	for key, values := range dimensionBiomes {
		dimensionBiomes[key] = uniqueTrimmed(values, 4096)
	}
	for key, values := range biomeDimensions {
		biomeDimensions[key] = uniqueTrimmed(values, 256)
	}
	return dimensionBiomes, biomeDimensions
}

func mergeImportedReferenceLists(current any, additional []string) []any {
	values := append(importedStringList(current), additional...)
	values = uniqueTrimmed(values, 4096)
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

func importedStringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return uniqueTrimmed(typed, 4096)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return uniqueTrimmed(result, 4096)
	case string:
		return uniqueTrimmed([]string{typed}, 1)
	default:
		return nil
	}
}

func canonicalResourceDefinitionFromTemplates(
	templates map[string]modContentTemplateDefinition,
	kindCode, resourcePath string,
	source map[string]any,
) (string, map[string]any, error) {
	entryTypeCode := inferImportedEntryTypeCode(kindCode, resourcePath, source)
	template, exists := templates[strings.ToLower(strings.TrimSpace(kindCode))]
	if !exists {
		return "default", map[string]any{}, nil
	}
	entryType, err := selectModContentEntryType(template.EntryTypes, entryTypeCode, kindCode, true)
	if err != nil {
		entryType, err = selectModContentEntryType(template.EntryTypes, "default", kindCode, true)
		entryTypeCode = "default"
	}
	if err != nil {
		return entryTypeCode, nil, err
	}
	canonical, err := canonicalModContentDefinition(*entryType, source, true)
	if err != nil && strings.EqualFold(strings.TrimSpace(kindCode), "minecraft.item") &&
		(entryTypeCode == "tool" || entryTypeCode == "equipment") {
		itemType, selectErr := selectModContentEntryType(template.EntryTypes, "item", kindCode, true)
		if selectErr == nil {
			if itemCanonical, fallbackErr := canonicalModContentDefinition(*itemType, source, true); fallbackErr == nil {
				return "item", itemCanonical, nil
			}
		}
	}
	return entryTypeCode, canonical, err
}

func canonicalizeGlobalCatalogResourceDefinition(
	ctx context.Context,
	query modContentTemplateRows,
	kindCode, canonicalID string,
	definition map[string]any,
) (map[string]any, error) {
	templates, err := loadBuiltinModContentTemplates(ctx, query)
	if err != nil {
		return nil, err
	}
	_, resourcePath, _ := strings.Cut(strings.TrimSpace(canonicalID), ":")
	_, canonical, err := canonicalResourceDefinitionFromTemplates(templates, kindCode, resourcePath, definition)
	return canonical, err
}

func loadBuiltinModContentTemplates(ctx context.Context, query modContentTemplateRows) (map[string]modContentTemplateDefinition, error) {
	rows, err := query.Query(ctx, `select definition from mod_content_templates where builtin and status='active' order by id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]modContentTemplateDefinition)
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var template modContentTemplateDefinition
		if err = json.Unmarshal(raw, &template); err != nil {
			return nil, err
		}
		for _, kindCode := range template.ResourceKinds {
			result[strings.ToLower(strings.TrimSpace(kindCode))] = template
		}
	}
	return result, rows.Err()
}

func inferImportedEntryTypeCode(kindCode, resourcePath string, source map[string]any) string {
	switch strings.ToLower(strings.TrimSpace(kindCode)) {
	case "minecraft.block":
		return "block"
	case "minecraft.item":
		path := strings.ToLower(strings.TrimSpace(resourcePath))
		for _, suffix := range []string{"helmet", "chestplate", "leggings", "boots", "elytra"} {
			if path == suffix || strings.HasSuffix(path, "_"+suffix) {
				return "equipment"
			}
		}
		if definitionHasAnyPath(source, [][]string{{"armor_value"}, {"armor_toughness"}, {"equipment_slot"}, {"equipment"}, {"armor"}}) {
			return "equipment"
		}
		if value, exists := modContentDefinitionValue(source, [][]string{{"max_damage"}, {"durability", "max_damage"}}); exists {
			if number, ok := catalogFiniteNumber(value); ok && number > 0 {
				return "tool"
			}
		}
		if definitionHasAnyPath(source, [][]string{{"tool"}, {"mining_speed"}, {"attack_damage"}, {"required_mining_level"}}) {
			return "tool"
		}
		return "item"
	case "minecraft.entity_type":
		return "entity"
	case "minecraft.enchantment":
		return "enchantment"
	case "minecraft.mob_effect", "minecraft.potion":
		return "mob_effect"
	case "minecraft.fluid":
		return "fluid"
	case "mod.skill":
		return "skill"
	case "minecraft.advancement":
		return "advancement"
	case "minecraft.loot_table":
		return "loot_table"
	case "minecraft.natural_generation":
		return "natural_generation"
	case "minecraft.structure":
		return "world_structure"
	default:
		if strings.HasPrefix(strings.ToLower(kindCode), "mekanism.") {
			return "chemical"
		}
		return "default"
	}
}

func definitionHasAnyPath(source map[string]any, paths [][]string) bool {
	for _, path := range paths {
		if _, exists := modContentDefinitionValue(source, [][]string{path}); exists {
			return true
		}
	}
	return false
}
