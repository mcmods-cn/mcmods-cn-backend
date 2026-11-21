package httpapi

import (
	"maps"
	"regexp"
	"strings"
)

// These rules are read-only after assembly. A vanilla export contains over a
// thousand loot tables and validates each pool against both of its source views.
var importedLootPools = buildImportedLootPools()

func exportLootJSONRule() *exportJSONRule { return importedLootPools }

func buildImportedLootPools() *exportJSONRule {
	// Unchanged fields from existing raw and normalized loot-pool contracts.
	// Register additions on their specific child role below.
	legacy := exportObjectRule(map[string]*exportJSONRule{})
	legacy.items = legacy
	legacy.strings, legacy.numbers, legacy.booleans = true, true, true
	exportFields(legacy, "rolls bonus_rolls entries conditions functions children terms term condition function type name weight quality value min max n p", legacy)
	exportFields(legacy, "count chance looting_multiplier unlooted looted levels enchantment enchantments treasure add formula parameters base multiplier limit damage", legacy)
	exportFields(legacy, "destination source target tool predicate predicates items item tag nbt components duration effects effect amplifier ambient show_particles show_icon", legacy)
	exportFields(legacy, "weather_check raining thundering time_check period killed_by_player inverse explosion_decay random_chance random_chance_with_looting survives_explosion", legacy)
	exportFields(legacy, "entity entity_properties entity_scores scores block_state_property block properties alternative alternatives sequence group location_check", legacy)
	exportFields(legacy, "offsetX offsetY offsetZ offset_x offset_y offset_z location biome dimension smokey light sky position x y z flags", legacy)
	exportFields(legacy, "is_on_fire is_sneaking is_sprinting is_swimming is_baby is_projectile is_explosion is_magic is_fire is_lightning is_creative_player direct_entity damage_source", legacy)
	exportFields(legacy, "food_level saturation will_drop head name_tag lore text translate extra with color bold italic underlined strikethrough obfuscated font text_component", legacy)
	exportFields(legacy, "set_contents contents slot slots operation uuid amount attribute modifiers", legacy)

	exportFields(legacy, "pool_index entry_index entry_type entry_kind item_id item_tag loot_table_id condition_types function_types entry_count child_count", legacy)
	exportFields(legacy, "normalization_status possible_item_ids possible_item_tags possible_items_complete possible_items_analysis unsupported_entry_type source_table_id source_path source_kind", legacy)
	for _, key := range []string{"enchantments", "scores", "properties"} {
		legacy.fields[key] = exportMapRule(exportScalarRule())
	}

	publicReference := exportResourceReferenceRule()
	resourceID := regexp.MustCompile(`^([a-z0-9_.-]+:)?[a-z0-9_.-]+(/[a-z0-9_.-]+)*$`)
	resource := &exportJSONRule{strings: true, validString: func(value string) bool {
		return resourceID.MatchString(value) && publicReference.validString(value)
	}}
	resourceOrTag := &exportJSONRule{strings: true, validString: func(value string) bool {
		return resource.validString(strings.TrimPrefix(value, "#"))
	}}
	number := exportNumberRule()
	numberRange := exportObjectRule(map[string]*exportJSONRule{"min": number, "max": number})
	numberRange.numbers = true
	propertyName := &exportJSONRule{strings: true, validString: regexp.MustCompile(`^[a-z0-9_]{1,128}$`).MatchString}
	propertyValue := &exportJSONRule{strings: true, numbers: true, booleans: true, validString: regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,128}$`).MatchString}
	state := exportMapRule(&exportJSONRule{
		fields:  map[string]*exportJSONRule{"min": propertyValue, "max": propertyValue},
		strings: true, validString: propertyValue.validString, numbers: true, booleans: true,
	})
	state.key = propertyName.validString

	// Item predicates describe required enchantments and level ranges. An
	// enchant_randomly function lists enchantment IDs. The older keyed form
	// carries scalar values and remains readable in both positions.
	enchantments := func(element *exportJSONRule) *exportJSONRule {
		return &exportJSONRule{values: exportScalarRule(), key: resource.validString, items: element}
	}
	role := func() *exportJSONRule { return exportObjectRule(maps.Clone(legacy.fields)) }
	predicate := role()
	predicate.fields["enchantments"] = enchantments(exportObjectRule(map[string]*exportJSONRule{
		"enchantment": resource, "levels": numberRange,
	}))
	typeSpecific := exportObjectRule(map[string]*exportJSONRule{
		"type": resource, "variant": resource, "size": numberRange, "in_open_water": exportBoolRule(),
	})
	predicate.fields["type_specific"] = typeSpecific
	predicate.fields["source_entity"] = exportObjectRule(map[string]*exportJSONRule{
		"type": resourceOrTag, "type_specific": typeSpecific,
	})
	predicate.fields["tags"] = exportArrayRule(exportObjectRule(map[string]*exportJSONRule{
		"id": resource, "expected": exportBoolRule(),
	}))
	predicate.fields["block"] = &exportJSONRule{
		strings: true, validString: resourceOrTag.validString,
		fields: map[string]*exportJSONRule{
			"blocks": exportArrayRule(resource), "state": state, "tag": resource,
			"nbt": exportStringRule(), "properties": state,
		},
	}

	condition := role()
	conditions := exportArrayRule(condition)
	condition.fields["predicate"] = predicate
	condition.fields["term"] = condition
	condition.fields["terms"] = conditions
	condition.fields["chances"] = exportArrayRule(number)
	condition.fields["properties"] = state

	function := role()
	function.fields["conditions"] = conditions
	function.fields["enchantments"] = enchantments(resource)
	function.fields["properties"] = &exportJSONRule{values: state.values, key: state.key, items: propertyName}
	function.fields["parameters"] = exportObjectRule(map[string]*exportJSONRule{
		"bonusMultiplier": number, "extra": number, "probability": number, "base": number, "multiplier": number,
	})
	exportFields(function, "id decoration", resource)
	function.fields["options"] = &exportJSONRule{strings: true, validString: func(value string) bool {
		id, tagged := strings.CutPrefix(value, "#")
		return tagged && resource.validString(id)
	}}
	exportFields(function, "zoom search_radius", number)
	function.fields["skip_existing_chunks"] = exportBoolRule()

	// copy_nbt is displayed as data. Accept named compound fields and numeric
	// list indices as paths. SNBT matchers and quoted free-form keys would need
	// a separate public projection before they can be retained.
	fieldPath := regexp.MustCompile(`^[A-Za-z0-9_+-]+(\[-?[0-9]+\])*(\.[A-Za-z0-9_+-]+(\[-?[0-9]+\])*)*$`)
	path := &exportJSONRule{strings: true, validString: func(value string) bool {
		return len(value) <= 256 && fieldPath.MatchString(value)
	}}
	function.fields["ops"] = exportArrayRule(exportObjectRule(map[string]*exportJSONRule{
		"source": path, "target": path, "op": {strings: true, validString: func(value string) bool {
			return value == "replace" || value == "append" || value == "merge"
		}},
	}))

	entry := role()
	entries := exportArrayRule(entry)
	functions := exportArrayRule(function)
	exportFields(entry, "dynamic_id tag_id", resource)
	entry.fields["expand"] = exportBoolRule()
	entry.fields["children"] = entries
	entry.fields["conditions"] = conditions
	entry.fields["functions"] = functions
	function.fields["entries"] = entries // set_contents uses raw child entries.

	pool := role()
	pool.fields["entries"] = entries
	pool.fields["conditions"] = conditions
	pool.fields["functions"] = functions
	return exportArrayRule(pool)
}
