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

func (s *Server) hydratePublicRecipeRenderLayouts(ctx context.Context, recipes []map[string]any) error {
	if err := s.hydrateRecipeRenderLayouts(ctx, recipes); err != nil {
		return err
	}
	return s.hydrateAuthoritativeRecipeRenderLayouts(ctx, recipes)
}

func stripPublicRecipeInternalFields(recipes []map[string]any) {
	for _, recipe := range recipes {
		delete(recipe, "_recipeEntityId")
		delete(recipe, "_authoritative")
	}
}

type authoritativeRecipeRenderState struct {
	recipe   map[string]any
	layout   map[string]any
	slots    []any
	slotByID map[int64]map[string]any
}

// hydrateAuthoritativeRecipeRenderLayouts projects the canonical editor model
// into the same public render shape as imported observations. It is set-based
// for the entire result page and never manufactures an import snapshot.
func (s *Server) hydrateAuthoritativeRecipeRenderLayouts(ctx context.Context, recipes []map[string]any) error {
	ids := make([]int64, 0, len(recipes))
	states := make(map[int64]*authoritativeRecipeRenderState, len(recipes))
	for _, recipe := range recipes {
		authoritative, _ := recipe["_authoritative"].(bool)
		if !authoritative {
			continue
		}
		id, _ := recipe["_recipeEntityId"].(int64)
		if id <= 0 {
			return fmt.Errorf("authoritative recipe identity is missing")
		}
		ids = append(ids, id)
		states[id] = &authoritativeRecipeRenderState{recipe: recipe, slotByID: make(map[int64]map[string]any)}
	}
	if len(ids) == 0 {
		return nil
	}

	rows, err := s.db.Query(ctx, `select recipe.entity_id,type.canonical_id,definition.definition,
		template.template_key,template.canvas_width,template.canvas_height,template.image_scale,
		slot.id,slot.slot_key,slot.role,slot.ordinal,slot.x::float8,slot.y::float8,slot.width::float8,slot.height::float8,slot.definition,
		binding.id,binding.definition,candidate.candidate_index,resource_entity.public_id,resource.kind_code,
		resource.canonical_id,resource.namespace,candidate.amount::float8,candidate.probability::float8,
		candidate.byproduct,candidate.definition,resource_entity.default_locale,
		coalesce((select jsonb_object_agg(localization.locale,localization.name) from content_localizations localization
			where localization.catalog_entity_id=resource_entity.id and localization.name<>''),'{}'::jsonb),
		coalesce(imported.revision_id,''),coalesce(imported.icon_path,'')
		from recipes recipe
		join recipe_definitions definition on definition.recipe_id=recipe.entity_id
		join recipe_types type on type.entity_id=recipe.recipe_type_id
		join recipe_layout_templates template on template.entity_id=definition.template_id
		join recipe_template_slots slot on slot.template_id=template.entity_id
		left join recipe_bindings binding on binding.recipe_id=recipe.entity_id and binding.template_slot_id=slot.id
		left join recipe_binding_candidates candidate on candidate.binding_id=binding.id
		left join game_resources resource on resource.entity_id=candidate.resource_id
		left join catalog_entities resource_entity on resource_entity.id=resource.entity_id
		left join lateral (select snapshot.revision_id,snapshot.icon_path from resource_import_snapshots snapshot
			join catalog_import_revisions revision on revision.id=snapshot.revision_id
			where snapshot.resource_id=resource.entity_id and revision.is_active and revision.status in ('ready','partial')
			order by (snapshot.icon_path<>'') desc,coalesce(revision.activated_at,revision.created_at) desc,snapshot.id desc limit 1) imported on true
		where recipe.entity_id=any($1::bigint[])
		order by recipe.entity_id,slot.ordinal,candidate.candidate_index`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var recipeID, slotID int64
		var recipeTypeID, templateKey, slotKey, role string
		var canvasWidth, canvasHeight, imageScale, ordinal int
		var x, y, width, height float64
		var recipeDefinition, slotDefinition []byte
		var bindingID, candidateIndex sql.NullInt64
		var bindingDefinition, candidateDefinition []byte
		var resourcePublicID, kindCode, canonicalID, namespace, defaultLocale, sourceRevisionID, iconPath sql.NullString
		var amount, probability sql.NullFloat64
		var byproduct sql.NullBool
		var names []byte
		if err = rows.Scan(&recipeID, &recipeTypeID, &recipeDefinition, &templateKey, &canvasWidth, &canvasHeight, &imageScale,
			&slotID, &slotKey, &role, &ordinal, &x, &y, &width, &height, &slotDefinition,
			&bindingID, &bindingDefinition, &candidateIndex, &resourcePublicID, &kindCode, &canonicalID, &namespace,
			&amount, &probability, &byproduct, &candidateDefinition, &defaultLocale, &names, &sourceRevisionID, &iconPath); err != nil {
			return err
		}
		state := states[recipeID]
		if state == nil {
			continue
		}
		if state.layout == nil {
			definition, decodeErr := decodeRecipeJSONObject(recipeDefinition, "authoritative definition")
			if decodeErr != nil {
				return decodeErr
			}
			layoutKind, _ := definition["layout_kind"].(string)
			if layoutKind == "" {
				layoutKind, _ = definition["layoutKind"].(string)
			}
			if layoutKind == "" {
				layoutKind = "unknown"
			}
			state.layout = map[string]any{
				"schema_version": "mcmods-canonical-recipe-render/v1", "layout_available": true,
				"layout_kind": layoutKind, "underlying_recipe_type_id": recipeTypeID,
				"template_id": templateKey, "coordinate_space": "logical_pixels", "image_scale": imageScale,
				"canvas": map[string]any{"width": canvasWidth, "height": canvasHeight}, "slots": state.slots,
				"background": "", "background_contains_ingredients": false,
			}
			state.recipe["layout"] = state.layout
		}
		slot := state.slotByID[slotID]
		if slot == nil {
			slot, err = decodeRecipeJSONObject(slotDefinition, "authoritative slot definition")
			if err != nil {
				return err
			}
			slot["slot_id"] = slotKey
			slot["role"] = role
			slot["ordinal"] = ordinal
			slot["coordinates_available"] = true
			slot["rect"] = map[string]any{"x": x, "y": y, "width": width, "height": height}
			slot["visual_rect"] = slot["rect"]
			slot["ingredient_present"] = bindingID.Valid
			slot["alternatives"] = []any{}
			if bindingID.Valid {
				bindingValue, decodeErr := decodeRecipeJSONObject(bindingDefinition, "authoritative binding definition")
				if decodeErr != nil {
					return decodeErr
				}
				slot["binding_definition"] = bindingValue
			}
			state.slotByID[slotID] = slot
			state.slots = append(state.slots, slot)
			state.layout["slots"] = state.slots
		}
		if !candidateIndex.Valid || !resourcePublicID.Valid || !canonicalID.Valid {
			continue
		}
		candidate, decodeErr := decodeRecipeJSONObject(candidateDefinition, "authoritative candidate definition")
		if decodeErr != nil {
			return decodeErr
		}
		candidate["alternative_index"] = int(candidateIndex.Int64)
		candidate["publicId"] = resourcePublicID.String
		candidate["id"] = canonicalID.String
		candidate["kind"] = kindCode.String
		candidate["registry"] = namespace.String
		candidate["amount"] = nullableSQLFloat(amount)
		candidate["probability"] = nullableSQLFloat(probability)
		candidate["chance_available"] = probability.Valid
		candidate["chance"] = nullableSQLFloat(probability)
		candidate["byproduct"] = byproduct.Valid && byproduct.Bool
		candidate["locale"] = defaultLocale.String
		candidate["names"] = json.RawMessage(names)
		candidate["sourceRevisionId"] = sourceRevisionID.String
		candidate["iconPath"] = iconPath.String
		alternatives, _ := slot["alternatives"].([]any)
		slot["alternatives"] = append(alternatives, candidate)
	}
	return rows.Err()
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
		var tagPublicID, tagRegistry, tagCanonicalID string
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
			&tagPublicID, &tagRegistry, &tagCanonicalID); err != nil {
			rows.Close()
			return err
		}
		state := states[snapshotID]
		if state == nil {
			continue
		}
		if state.layout == nil {
			parametersValue, decodeErr := decodeRecipeJSONObject(parameters, "import parameters")
			if decodeErr != nil {
				rows.Close()
				return decodeErr
			}
			canvasValue, decodeErr := decodeRecipeJSONObject(canvas, "import canvas")
			if decodeErr != nil {
				rows.Close()
				return decodeErr
			}
			imagePixelsValue, decodeErr := decodeRecipeJSONObject(imagePixels, "import image_pixels")
			if decodeErr != nil {
				rows.Close()
				return decodeErr
			}
			contentValue, decodeErr := decodeRecipeJSONObject(contentRect, "import content")
			if decodeErr != nil {
				rows.Close()
				return decodeErr
			}
			state.layout = map[string]any{
				"schema_version":                  "mcmods-recipe-render/v1",
				"layout_available":                layoutAvailable,
				"layout_kind":                     layoutKind,
				"ordered":                         nullableSQLBool(ordered),
				"layout_classification_source":    classificationSource,
				"width":                           nullableSQLInt(width),
				"height":                          nullableSQLInt(height),
				"parameters":                      parametersValue,
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
				"canvas":                          canvasValue,
				"image_pixels":                    imagePixelsValue,
				"content":                         contentValue,
				"slots":                           state.slots,
			}
			state.recipe["layout"] = state.layout
		}
		if slotDatabaseID == "" {
			continue
		}
		slot, decodeErr := decodeRecipeJSONObject(slotData, "import slot data")
		if decodeErr != nil {
			rows.Close()
			return decodeErr
		}
		rectValue, decodeErr := decodeRecipeJSONObject(rect, "import slot rect")
		if decodeErr != nil {
			rows.Close()
			return decodeErr
		}
		visualRectValue, decodeErr := decodeRecipeJSONObject(visualRect, "import slot visual_rect")
		if decodeErr != nil {
			rows.Close()
			return decodeErr
		}
		slot["slot_id"] = sourceSlotID
		slot["role"] = role
		slot["ordinal"] = slotOrdinal
		slot["coordinates_available"] = coordinatesAvailable
		slot["rect"] = rectValue
		slot["visual_rect"] = visualRectValue
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
			slot["tagPublicId"] = tagPublicID
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
		chanceTextValues, decodeErr := decodeRecipeJSONObject(chanceTexts, "import candidate chance_texts")
		if decodeErr != nil {
			return decodeErr
		}
		alternative["chance_texts"] = chanceTextValues
		alternative["chance_translation_key"] = chanceTranslationKey
		alternative["chance_render_x"] = nullableSQLFloat(chanceRenderX)
		alternative["chance_render_y"] = nullableSQLFloat(chanceRenderY)
		alternative["byproduct"] = byproduct
		alternatives, _ := slot["alternatives"].([]any)
		slot["alternatives"] = append(alternatives, alternative)
	}
	return alternativeRows.Err()
}

func decodeRecipeJSONObject(raw []byte, label string) (map[string]any, error) {
	result, err := decodeStoredJSONObject(raw, label)
	if err != nil {
		return nil, fmt.Errorf("decode recipe %s: %w", label, err)
	}
	return result, nil
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
	return decodeRecipeJSONObject(raw, "layout override")
}
