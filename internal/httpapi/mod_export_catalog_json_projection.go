package httpapi

import "strings"

// Only importer rows pass this boundary. Human-authored editor definitions
// retain their separate explicit schema and authorization contract.
func projectImportedCatalogJSONFields(templates map[string]modContentTemplateDefinition, kindCode, entryTypeCode string, source, definition map[string]any) error {
	template, ok := templates[strings.ToLower(strings.TrimSpace(kindCode))]
	if !ok {
		return nil
	}
	entryType, err := selectModContentEntryType(template.EntryTypes, entryTypeCode, kindCode, true, false)
	if err != nil {
		return err
	}
	for _, group := range entryType.Groups {
		for _, field := range group.Fields {
			if field.Type != "json" {
				continue
			}
			value, exists := definition[field.Code]
			if !exists {
				continue
			}
			rule := registeredImportedCatalogJSONRule(field.Code)
			if rule == nil {
				return exportAssetRegistrationError("catalog_json_field", kindCode+"/"+field.Code)
			}
			paths := append(append([][]string{}, field.Paths...), []string{field.Code})
			if original, exists := modContentDefinitionValue(source, paths); exists {
				if _, err := rule.project(original); err != nil {
					return exportAssetRegistrationError("catalog_json_field", kindCode+"/"+field.Code)
				}
			}
			projected, err := rule.project(value)
			if err != nil {
				return exportAssetRegistrationError("catalog_json_field", kindCode+"/"+field.Code)
			}
			definition[field.Code] = projected
		}
	}
	return nil
}

func registeredImportedCatalogJSONRule(code string) *exportJSONRule {
	switch code {
	case "display":
		return exportDisplayJSONRule()
	case "requirements":
		return exportArrayRule(exportArrayRule(exportStringRule()))
	case "rewards":
		return exportAdvancementJSONRule(true).fields["rewards"]
	case "categoryNames":
		return exportMapRule(exportStringRule())
	case "defaultState":
		return exportMapRule(exportScalarRule())
	case "attributeModifiers", "combatModifiers", "effectAttributeModifiers":
		modifier := exportObjectRule(map[string]*exportJSONRule{})
		exportFields(modifier, "attribute uuid id name operation slot", exportStringRule())
		exportFields(modifier, "amount operation_id base_amount amount_at_amplifier_0 amount_at_amplifier_1", exportNumberRule())
		return exportArrayRule(exportObjectRule(map[string]*exportJSONRule{"slot": exportStringRule(), "modifiers": exportArrayRule(modifier), "attribute": exportStringRule(), "uuid": exportStringRule(), "id": exportStringRule(), "name": exportStringRule(), "operation": exportStringRule(), "operation_id": exportNumberRule(), "amount": exportNumberRule(), "base_amount": exportNumberRule(), "amount_at_amplifier_0": exportNumberRule(), "amount_at_amplifier_1": exportNumberRule()}))
	case "defaultEquipment":
		return exportArrayRule(exportObjectRule(map[string]*exportJSONRule{"slot": exportStringRule(), "item": exportStringRule(), "count": exportNumberRule(), "nbt_snbt": exportStringRule(), "nbt": exportStringRule()}))
	case "costs":
		rule := exportObjectRule(map[string]*exportJSONRule{})
		rule.items = rule
		exportFields(rule, "level min max min_cost max_cost minimum maximum", exportNumberRule())
		return rule
	case "spawnData":
		return exportMapRule(exportArrayRule(exportObjectRule(map[string]*exportJSONRule{"type": exportStringRule(), "weight": exportNumberRule(), "minCount": exportNumberRule(), "maxCount": exportNumberRule(), "min_count": exportNumberRule(), "max_count": exportNumberRule()})))
	case "effects":
		rule := exportObjectRule(map[string]*exportJSONRule{})
		exportFields(rule, "fog_color water_color water_fog_color sky_color foliage_color grass_color", exportNumberRule())
		exportFields(rule, "grass_color_modifier ambient_sound", exportStringRule())
		rule.fields["mood_sound"] = exportObjectRule(map[string]*exportJSONRule{"sound": exportStringRule(), "tick_delay": exportNumberRule(), "block_search_extent": exportNumberRule(), "offset": exportNumberRule()})
		rule.fields["additions_sound"] = exportObjectRule(map[string]*exportJSONRule{"sound": exportStringRule(), "tick_chance": exportNumberRule()})
		rule.fields["music"] = exportObjectRule(map[string]*exportJSONRule{"sound": exportStringRule(), "min_delay": exportNumberRule(), "max_delay": exportNumberRule(), "replace_current_music": exportBoolRule()})
		rule.fields["particle"] = exportObjectRule(map[string]*exportJSONRule{"probability": exportNumberRule(), "options": exportObjectRule(map[string]*exportJSONRule{"type": exportStringRule(), "color": exportArrayRule(exportNumberRule()), "scale": exportNumberRule(), "block_state": exportWorldgenDefinitionJSONRule(), "item": exportObjectRule(map[string]*exportJSONRule{"id": exportStringRule(), "Count": exportNumberRule(), "tag": exportStringRule()})})})
		return rule
	case "pools":
		return exportLootJSONRule()
	case "generatorSettings", "biomeSource", "outputs", "targets", "size", "placement", "biomeSelectors", "startHeight":
		return exportWorldgenDefinitionJSONRule()
	}
	return nil
}

func exportLootJSONRule() *exportJSONRule {
	rule := exportObjectRule(map[string]*exportJSONRule{})
	rule.items = rule
	rule.strings = true
	rule.numbers = true
	rule.booleans = true
	// Vanilla loot condition/function/provider vocabulary. Mod codec properties
	// outside this vocabulary require a registered adapter and golden sample.
	exportFields(rule, "rolls bonus_rolls entries conditions functions children terms term condition function type name weight quality value min max n p count chance looting_multiplier unlooted looted levels enchantment enchantments treasure add formula parameters base multiplier limit damage destination source target tool predicate predicates items item tag nbt components duration effects effect amplifier ambient show_particles show_icon weather_check raining thundering time_check value period killed_by_player inverse explosion_decay random_chance random_chance_with_looting survives_explosion entity entity_properties entity_scores scores block_state_property block properties alternative alternatives sequence group location_check offsetX offsetY offsetZ offset_x offset_y offset_z location biome dimension smokey light sky block position x y z flags is_on_fire is_sneaking is_sprinting is_swimming is_baby is_projectile is_explosion is_magic is_fire is_lightning is_creative_player direct_entity damage_source food_level saturation will_drop head name_tag lore text translate extra with color bold italic underlined strikethrough obfuscated font text_component set_contents contents slot slots operation uuid amount attribute modifiers", rule)
	exportFields(rule, "pool_index entry_index entry_type entry_kind item_id item_tag loot_table_id condition_types function_types entry_count child_count normalization_status possible_item_ids possible_item_tags possible_items_complete possible_items_analysis unsupported_entry_type source_table_id source_path source_kind", rule)
	// Resource-keyed enchantment/score/property maps carry scalar business
	// values, never arbitrary nested diagnostic objects.
	for _, key := range []string{"enchantments", "scores", "properties"} {
		rule.fields[key] = exportMapRule(exportScalarRule())
	}
	return rule
}
