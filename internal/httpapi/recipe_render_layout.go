package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type recipeRenderState struct {
	recipe   map[string]any
	layout   map[string]any
	slots    []any
	bindings map[string]map[string]any
}

// hydrateRecipeRenderLayouts builds the API render model from normalized JEI
// templates and bindings in two set-based queries for the current result page.
func (s *Server) hydrateRecipeRenderLayouts(ctx context.Context, recipes []map[string]any) error {
	snapshotIDs := make([]string, 0, len(recipes))
	states := make(map[string]*recipeRenderState, len(recipes))
	for _, recipe := range recipes {
		if _, overridden := recipe["layout"]; overridden {
			continue
		}
		snapshotID, _ := recipe["recipeSnapshotId"].(string)
		if snapshotID == "" {
			continue
		}
		snapshotIDs = append(snapshotIDs, snapshotID)
		states[snapshotID] = &recipeRenderState{recipe: recipe, bindings: make(map[string]map[string]any)}
	}
	if len(snapshotIDs) == 0 {
		return nil
	}

	rows, err := s.db.Query(ctx, `select snapshot.id,snapshot.layout_available,snapshot.layout_kind,snapshot.ordered,
		snapshot.layout_classification_source,snapshot.width,snapshot.height,snapshot.parameters,
		snapshot.origin_kind,snapshot.underlying_recipe_type_id,snapshot.source_mod_id,snapshot.source_mod_version,
		snapshot.source_mod_id_source,snapshot.render_locale,
		coalesce(template.source_template_id,''),coalesce(template.background_path,''),
		coalesce(template.background_contains_ingredients,false),coalesce(template.coordinate_space,'logical_pixels'),
		coalesce(template.image_scale,1),coalesce(template.canvas,'{}'::jsonb),
		coalesce(template.image_pixels,'{}'::jsonb),coalesce(template.content_rect,'{}'::jsonb),
		coalesce(slot.id,''),coalesce(slot.source_slot_id,''),coalesce(slot.role,''),coalesce(slot.ordinal,0),
		coalesce(slot.coordinates_available,false),coalesce(slot.rect,'{}'::jsonb),
		coalesce(slot.visual_rect,'{}'::jsonb),coalesce(slot.data,'{}'::jsonb),
		coalesce(binding.id,''),coalesce(binding.ingredient_present,false),coalesce(binding.clickable,false),
		coalesce(binding.placeholder_item,''),coalesce(binding.item_tag_equivalent,''),coalesce(binding.semantic_role,''),
		coalesce(binding.role_source,''),
		coalesce(tag_entity.public_id,''),coalesce(tag.registry,''),coalesce(tag.canonical_id,'')
		from recipe_import_snapshots snapshot
		left join recipe_template_import_snapshots template on template.id=snapshot.template_id
		left join recipe_template_import_slots slot on slot.template_id=template.id
		left join recipe_import_bindings binding on binding.recipe_snapshot_id=snapshot.id and binding.template_slot_id=slot.id
		left join catalog_tags tag on tag.entity_id=binding.tag_id
		left join catalog_entities tag_entity on tag_entity.id=tag.entity_id
		where snapshot.id=any($1::text[])
		order by snapshot.id,slot.ordinal`, snapshotIDs)
	if err != nil {
		return err
	}
	for rows.Next() {
		var snapshotID, layoutKind, classificationSource string
		var originKind, underlyingRecipeTypeID, sourceModID, sourceModVersion, sourceModIDSource, renderLocale string
		var sourceTemplateID, backgroundPath, coordinateSpace string
		var slotDatabaseID, sourceSlotID, role, bindingID string
		var placeholderItem, itemTagEquivalent, semanticRole, roleSource string
		var tagEntityID, tagRegistry, tagCanonicalID string
		var layoutAvailable, backgroundContainsIngredients, coordinatesAvailable bool
		var ingredientPresent, clickable bool
		var imageScale, slotOrdinal int
		var ordered sql.NullBool
		var width, height sql.NullInt64
		var parameters, canvas, imagePixels, contentRect, rect, visualRect, slotData []byte
		if err = rows.Scan(&snapshotID, &layoutAvailable, &layoutKind, &ordered, &classificationSource, &width, &height,
			&parameters, &originKind, &underlyingRecipeTypeID, &sourceModID, &sourceModVersion, &sourceModIDSource,
			&renderLocale, &sourceTemplateID, &backgroundPath, &backgroundContainsIngredients, &coordinateSpace,
			&imageScale, &canvas, &imagePixels, &contentRect, &slotDatabaseID, &sourceSlotID, &role, &slotOrdinal,
			&coordinatesAvailable, &rect, &visualRect, &slotData, &bindingID, &ingredientPresent, &clickable,
			&placeholderItem, &itemTagEquivalent, &semanticRole, &roleSource,
			&tagEntityID, &tagRegistry, &tagCanonicalID); err != nil {
			rows.Close()
			return err
		}
		state := states[snapshotID]
		if state == nil {
			continue
		}
		if state.layout == nil {
			state.layout = map[string]any{
				"schema_version":                  "mcmods-recipe-render/v1",
				"layout_available":                layoutAvailable,
				"layout_kind":                     layoutKind,
				"ordered":                         nullableSQLBool(ordered),
				"layout_classification_source":    classificationSource,
				"width":                           nullableSQLInt(width),
				"height":                          nullableSQLInt(height),
				"parameters":                      decodeJSONObject(parameters),
				"origin_kind":                     originKind,
				"underlying_recipe_type_id":       underlyingRecipeTypeID,
				"source_mod_id":                   sourceModID,
				"source_mod_version":              sourceModVersion,
				"source_mod_id_source":            sourceModIDSource,
				"render_locale":                   renderLocale,
				"template_id":                     sourceTemplateID,
				"background":                      backgroundPath,
				"background_contains_ingredients": backgroundContainsIngredients,
				"coordinate_space":                coordinateSpace,
				"image_scale":                     imageScale,
				"canvas":                          decodeJSONObject(canvas),
				"image_pixels":                    decodeJSONObject(imagePixels),
				"content":                         decodeJSONObject(contentRect),
				"slots":                           state.slots,
			}
			state.recipe["layout"] = state.layout
		}
		if slotDatabaseID == "" {
			continue
		}
		slot := decodeJSONObject(slotData)
		slot["slot_id"] = sourceSlotID
		slot["role"] = role
		slot["ordinal"] = slotOrdinal
		slot["coordinates_available"] = coordinatesAvailable
		slot["rect"] = decodeJSONObject(rect)
		slot["visual_rect"] = decodeJSONObject(visualRect)
		slot["ingredient_present"] = ingredientPresent
		slot["clickable"] = clickable
		slot["placeholder_item"] = placeholderItem
		slot["item_tag_equivalent"] = itemTagEquivalent
		slot["semantic_role"] = semanticRole
		slot["role_source"] = roleSource
		// Probability is an attribute of each output item. The exporter source
		// stores it beside the slot, so remove those raw keys here and attach the
		// normalized values to each candidate in the query below.
		for _, key := range []string{"chance_available", "chance", "chance_percent", "chance_comparator", "chance_source",
			"chance_text", "chance_texts", "chance_translation_key", "chance_render_x", "chance_render_y", "byproduct"} {
			delete(slot, key)
		}
		slot["alternatives"] = []any{}
		if tagCanonicalID != "" {
			slot["tag"] = tagCanonicalID
			slot["tagEntityId"] = tagEntityID
			slot["tagRegistry"] = tagRegistry
		}
		state.slots = append(state.slots, slot)
		state.layout["slots"] = state.slots
		if bindingID != "" {
			state.bindings[bindingID] = slot
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	bindingStates := make(map[string]map[string]any)
	for _, state := range states {
		for bindingID, slot := range state.bindings {
			bindingStates[bindingID] = slot
		}
	}
	if len(bindingStates) == 0 {
		return nil
	}
	bindingIDs := make([]string, 0, len(bindingStates))
	for bindingID := range bindingStates {
		bindingIDs = append(bindingIDs, bindingID)
	}
	alternativeRows, err := s.db.Query(ctx, `select binding_id,alternative_index,raw_resource_id,amount,
		ingredient_kind,ingredient_type,unique_id,nbt_snbt,chance_available,chance,chance_percent,
		chance_comparator,chance_source,chance_text,chance_texts,chance_translation_key,chance_render_x,chance_render_y,byproduct
		from recipe_import_binding_candidates where binding_id=any($1::text[])
		order by binding_id,alternative_index`, bindingIDs)
	if err != nil {
		return err
	}
	defer alternativeRows.Close()
	for alternativeRows.Next() {
		var bindingID, resourceID, ingredientKind, ingredientType, uniqueID, nbtSNBT string
		var chanceComparator, chanceSource, chanceText, chanceTranslationKey string
		var alternativeIndex int
		var amount float64
		var chanceAvailable, byproduct bool
		var chance, chancePercent, chanceRenderX, chanceRenderY sql.NullFloat64
		var chanceTexts []byte
		if err = alternativeRows.Scan(&bindingID, &alternativeIndex, &resourceID, &amount, &ingredientKind,
			&ingredientType, &uniqueID, &nbtSNBT, &chanceAvailable, &chance, &chancePercent,
			&chanceComparator, &chanceSource, &chanceText, &chanceTexts, &chanceTranslationKey,
			&chanceRenderX, &chanceRenderY, &byproduct); err != nil {
			return err
		}
		slot := bindingStates[bindingID]
		if slot == nil {
			continue
		}
		alternative := map[string]any{}
		alternative["alternative_index"] = alternativeIndex
		alternative["resource_location"] = resourceID
		alternative["amount"] = amount
		alternative["ingredient_kind"] = ingredientKind
		alternative["ingredient_type"] = ingredientType
		alternative["unique_id"] = uniqueID
		alternative["nbt_snbt"] = nbtSNBT
		alternative["chance_available"] = chanceAvailable
		alternative["chance"] = nullableSQLFloat(chance)
		alternative["chance_percent"] = nullableSQLFloat(chancePercent)
		alternative["chance_comparator"] = chanceComparator
		alternative["chance_source"] = chanceSource
		alternative["chance_text"] = chanceText
		alternative["chance_texts"] = decodeJSONObject(chanceTexts)
		alternative["chance_translation_key"] = chanceTranslationKey
		alternative["chance_render_x"] = nullableSQLFloat(chanceRenderX)
		alternative["chance_render_y"] = nullableSQLFloat(chanceRenderY)
		alternative["byproduct"] = byproduct
		alternatives, _ := slot["alternatives"].([]any)
		slot["alternatives"] = append(alternatives, alternative)
	}
	return alternativeRows.Err()
}

func decodeJSONObject(raw []byte) map[string]any {
	result := make(map[string]any)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &result)
	}
	return result
}

func nullableSQLBool(value sql.NullBool) any {
	if !value.Valid {
		return nil
	}
	return value.Bool
}

func nullableSQLInt(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return int(value.Int64)
}

func nullableSQLFloat(value sql.NullFloat64) any {
	if !value.Valid {
		return nil
	}
	return value.Float64
}

func scanOptionalRecipeOverride(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var layout map[string]any
	if err := json.Unmarshal(raw, &layout); err != nil {
		return nil, fmt.Errorf("decode recipe layout override: %w", err)
	}
	return layout, nil
}
