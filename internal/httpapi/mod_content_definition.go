package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
)

const modContentDefinitionSchemaVersion = 1

type modContentTemplateRows interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// loadModContentSectionDefinition returns the website-wide schema used by a
// version-scoped data page. Child classification sections resolve to the same
// root template; mods and versions never own separate attribute contracts.
func loadModContentSectionDefinition(
	ctx context.Context,
	query modContentImageQuerier,
	modID, versionID int64,
	sectionPublicID string,
	destination *[]byte,
) error {
	return query.QueryRow(ctx, `with recursive lineage as (
		select section.id,section.parent_id,section.template_id
		from mod_content_sections section
		where section.public_id=$1 and section.mod_id=$2 and section.version_id=$3 and section.status='active'
		union all
		select parent.id,parent.parent_id,parent.template_id
		from mod_content_sections parent join lineage child on child.parent_id=parent.id
		where parent.mod_id=$2 and parent.version_id=$3 and parent.status='active'
	), root as (
		select * from lineage where parent_id is null limit 1
	)
	select template.definition
	from root join mod_content_templates template on template.id=root.template_id and template.status='active'`,
		sectionPublicID, modID, versionID).Scan(destination)
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
	entryType, err := loadModContentEntryTypeDefinition(
		ctx, query, modID, versionID, kindCode, sectionPublicID, entryTypeCode,
	)
	if err != nil {
		return nil, err
	}
	normalized, err := canonicalModContentDefinition(*entryType, definition, useImportAliases)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(strings.TrimSpace(kindCode), "minecraft.loot_table") {
		normalizeLootTableCanonicalDefinition(normalized)
		encoded, encodeErr := json.Marshal(normalized)
		if encodeErr != nil || len(encoded) > 512*1024 || catalogJSONDepth(normalized, 0) > 20 {
			return nil, errCatalogEditorInvalid
		}
	}
	if strings.EqualFold(strings.TrimSpace(kindCode), "minecraft.advancement") {
		if groupID, ok := normalizeAdvancementLayoutGroupID(definition["layoutGroupId"]); ok {
			normalized["layoutGroupId"] = groupID
		}
	}
	return normalized, nil
}

func loadModContentEntryTypeDefinition(
	ctx context.Context,
	query modContentImageQuerier,
	modID, versionID int64,
	kindCode string,
	sectionPublicID *string,
	entryTypeCode string,
) (*modContentEntryTypeDefinition, error) {
	if sectionPublicID == nil || strings.TrimSpace(*sectionPublicID) == "" {
		if strings.EqualFold(strings.TrimSpace(entryTypeCode), "default") {
			return &modContentEntryTypeDefinition{Code: "default"}, nil
		}
		return nil, errCatalogEditorReference
	}
	var raw []byte
	if err := loadModContentSectionDefinition(ctx, query, modID, versionID, *sectionPublicID, &raw); err != nil {
		return nil, err
	}
	var template modContentTemplateDefinition
	if err := json.Unmarshal(raw, &template); err != nil {
		return nil, err
	}
	return selectModContentEntryType(template.EntryTypes, entryTypeCode, kindCode, false)
}

func validateModContentEditableDefinitionPatch(
	ctx context.Context,
	query modContentImageQuerier,
	modID, versionID int64,
	kindCode string,
	sectionPublicID *string,
	entryTypeCode string,
	patch map[string]any,
) error {
	entryType, err := loadModContentEntryTypeDefinition(
		ctx, query, modID, versionID, kindCode, sectionPublicID, entryTypeCode,
	)
	if err != nil {
		return err
	}
	return validateModContentEntryDefinitionPatch(*entryType, patch)
}

func validateModContentEntryDefinitionPatch(entryType modContentEntryTypeDefinition, patch map[string]any) error {
	for _, group := range entryType.Groups {
		for _, field := range group.Fields {
			if field.Editable == nil || *field.Editable {
				continue
			}
			if _, changed := patch[field.Code]; changed {
				return fmt.Errorf("%w: field %s is read-only", errCatalogEditorInvalid, field.Code)
			}
		}
	}
	return nil
}

func normalizeLootTableCanonicalDefinition(definition map[string]any) {
	source := make(map[string]any, len(definition))
	for key, value := range definition {
		source[key] = value
	}
	for _, key := range []string{"possibleItemIds", "possible_item_ids", "referencedLootTables", "referenced_loot_tables"} {
		delete(source, key)
	}
	if itemIDs := lootTableItemIDs(source); len(itemIDs) > 0 {
		definition["possibleItemIds"] = itemIDs
	} else {
		delete(definition, "possibleItemIds")
	}
	if referenceIDs := lootTableReferenceIDs(source); len(referenceIDs) > 0 {
		definition["referencedLootTables"] = referenceIDs
	} else {
		delete(definition, "referencedLootTables")
	}
	_, hasPools := definition["pools"]
	definition["definitionAvailable"] = hasPools
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
		if !modContentEntryTypeEnabled(*entryType) {
			return nil, errCatalogEditorReference
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

func modContentEntryTypeEnabled(entryType modContentEntryTypeDefinition) bool {
	return entryType.Enabled == nil || *entryType.Enabled
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

// mergeModContentDefinitionPatch applies the editor's JSON merge-patch-shaped
// payload to the currently stored canonical document. Editors only submit
// changed fields; a null value explicitly removes a field.
func mergeModContentDefinitionPatch(base, patch map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(patch))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range patch {
		if value == nil {
			delete(result, key)
			continue
		}
		patchObject, patchIsObject := value.(map[string]any)
		if !patchIsObject {
			result[key] = value
			continue
		}
		baseObject, _ := result[key].(map[string]any)
		result[key] = mergeModContentDefinitionPatch(baseObject, patchObject)
	}
	return result
}

func normalizeModContentFieldValue(field modContentEntryTypeField, value any) (any, bool, error) {
	switch field.Type {
	case "number":
		number, ok := catalogFiniteNumber(value)
		if !ok {
			return nil, false, errCatalogEditorInvalid
		}
		if catalogStringIn(field.Format, "integer", "health", "armor") && number != math.Trunc(number) {
			return nil, false, errCatalogEditorInvalid
		}
		return number, true, nil
	case "range":
		items, ok := value.([]any)
		if !ok || len(items) != 2 {
			return nil, false, errCatalogEditorInvalid
		}
		minimum, minimumOK := catalogFiniteNumber(items[0])
		maximum, maximumOK := catalogFiniteNumber(items[1])
		if !minimumOK || !maximumOK || minimum > maximum {
			return nil, false, errCatalogEditorInvalid
		}
		return []float64{minimum, maximum}, true, nil
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
	advancementGroups := importedAdvancementLayoutGroups(rows)
	for index := range rows {
		row := &rows[index]
		var source map[string]any
		if decodeErr := json.Unmarshal([]byte(nonEmptyJSONObject(row.Data)), &source); decodeErr != nil {
			return fmt.Errorf("decode %s definition: %w", row.CanonicalID, decodeErr)
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
			log.Printf("resource document %s matched configured type %s instead of default inference %s", row.CanonicalID, entryTypeCode, inferredEntryTypeCode)
		}
		if groupID := advancementGroups[strings.ToLower(strings.TrimSpace(row.CanonicalID))]; groupID != "" {
			canonical["layoutGroupId"] = groupID
		}
		encoded, _ := json.Marshal(canonical)
		row.EntryTypeCode = entryTypeCode
		row.DefinitionSchemaVersion = modContentDefinitionSchemaVersion
		row.Data = string(encoded)
	}
	return nil
}

func importedAdvancementLayoutGroups(rows []catalogResourceImportRow) map[string]string {
	parents := make(map[string]string)
	canonicalIDs := make([]string, 0)
	for _, row := range rows {
		if !strings.EqualFold(strings.TrimSpace(row.KindCode), "minecraft.advancement") {
			continue
		}
		canonicalID := strings.ToLower(strings.TrimSpace(row.CanonicalID))
		if canonicalID == "" {
			continue
		}
		canonicalIDs = append(canonicalIDs, canonicalID)
		var source map[string]any
		if json.Unmarshal([]byte(nonEmptyJSONObject(row.Data)), &source) == nil {
			parent, _ := source["parentId"].(string)
			if strings.TrimSpace(parent) == "" {
				parent, _ = source["parent"].(string)
			}
			parents[canonicalID] = strings.ToLower(strings.TrimSpace(parent))
		}
	}
	neighbors := make(map[string][]string, len(canonicalIDs))
	known := make(map[string]struct{}, len(canonicalIDs))
	for _, canonicalID := range canonicalIDs {
		known[canonicalID] = struct{}{}
	}
	for childID, parentID := range parents {
		if parentID == "" || parentID == childID {
			continue
		}
		if _, exists := known[parentID]; !exists {
			continue
		}
		neighbors[childID] = append(neighbors[childID], parentID)
		neighbors[parentID] = append(neighbors[parentID], childID)
	}
	sort.Strings(canonicalIDs)
	result := make(map[string]string, len(canonicalIDs))
	for _, canonicalID := range canonicalIDs {
		if result[canonicalID] != "" {
			continue
		}
		groupID := "advancement:" + canonicalID
		pending := []string{canonicalID}
		result[canonicalID] = groupID
		for cursor := 0; cursor < len(pending); cursor++ {
			for _, neighborID := range neighbors[pending[cursor]] {
				if result[neighborID] != "" {
					continue
				}
				result[neighborID] = groupID
				pending = append(pending, neighborID)
			}
		}
	}
	return result
}

func normalizeAdvancementLayoutGroupID(value any) (string, bool) {
	groupID, ok := value.(string)
	groupID = strings.TrimSpace(groupID)
	if !ok || groupID == "" || len([]rune(groupID)) > 512 {
		return "", false
	}
	for _, character := range groupID {
		if unicode.IsControl(character) {
			return "", false
		}
	}
	return groupID, true
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
	template, exists := templates[strings.ToLower(strings.TrimSpace(kindCode))]
	if !exists {
		return "default", map[string]any{}, nil
	}
	entryTypeCode := matchImportedEntryType(template, kindCode, source, inferImportedEntryTypeCode(kindCode, resourcePath, source))
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

func matchImportedEntryType(
	template modContentTemplateDefinition,
	kindCode string,
	source map[string]any,
	fallback string,
) string {
	type matchScore struct {
		code        string
		distinctive int
		matched     int
	}
	candidates := make([]modContentEntryTypeDefinition, 0, len(template.EntryTypes))
	pathOwners := make(map[string]int)
	for _, entryType := range template.EntryTypes {
		if strings.EqualFold(strings.TrimSpace(entryType.Code), "default") || !modContentEntryTypeEnabled(entryType) || !modContentEntryTypeSupportsKind(entryType, kindCode) {
			continue
		}
		candidates = append(candidates, entryType)
		seen := make(map[string]struct{})
		for _, group := range entryType.Groups {
			for _, field := range group.Fields {
				for _, path := range field.Paths {
					key := strings.Join(path, "\x00")
					if _, exists := seen[key]; !exists {
						seen[key] = struct{}{}
						pathOwners[key]++
					}
				}
			}
		}
	}
	best := matchScore{code: fallback}
	for _, entryType := range candidates {
		score := matchScore{code: entryType.Code}
		for _, group := range entryType.Groups {
			for _, field := range group.Fields {
				matched := false
				distinctive := false
				for _, path := range field.Paths {
					if _, exists := modContentDefinitionValue(source, [][]string{path}); !exists {
						continue
					}
					matched = true
					if pathOwners[strings.Join(path, "\x00")] == 1 {
						distinctive = true
					}
				}
				if matched {
					score.matched++
				}
				if distinctive {
					score.distinctive++
				}
			}
		}
		if strings.EqualFold(strings.TrimSpace(score.code), strings.TrimSpace(fallback)) {
			continue
		}
		if score.distinctive > best.distinctive || (score.distinctive == best.distinctive && score.distinctive > 0 && score.matched > best.matched) {
			best = score
		}
	}
	return best.code
}

func modContentEntryTypeSupportsKind(entryType modContentEntryTypeDefinition, kindCode string) bool {
	if len(entryType.KindCodes) == 0 {
		return true
	}
	for _, candidate := range entryType.KindCodes {
		if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(kindCode)) {
			return true
		}
	}
	return false
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
	case "minecraft.dimension":
		return "dimension"
	case "minecraft.biome":
		return "biome"
	case "minecraft.key_mapping":
		return "key_mapping"
	case "minecraft.command":
		return "command"
	case "minecraft.multiblock":
		return "multiblock"
	case "minecraft.game_setting":
		return "game_setting"
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
