package httpapi

import "regexp"

// WorldgenDataStage writes a placement summary and copies the 1.20.1
// PlacementModifier codecs into modifiers. Their fields carry different JSON
// types from the summary (e.g. a provider distribution versus its name).
var naturalGenerationRules = newNaturalGenerationRules()

type naturalGenerationJSONRules struct {
	placement     *exportJSONRule
	placedFeature *exportJSONRule
	carverFields  map[string]*exportJSONRule
}

func newNaturalGenerationRules() naturalGenerationJSONRules {
	id := exportResourceReferenceRule()
	integer := &exportJSONRule{integers: true}
	number := exportNumberRule()
	identifiers := &exportJSONRule{strings: true, validString: id.validString, items: id}
	token := regexp.MustCompile(`^[a-z0-9_]+$`).MatchString
	state := exportObjectRule(map[string]*exportJSONRule{
		"Name": id, "Properties": {key: token, values: &exportJSONRule{strings: true, validString: token}},
	})
	predicate := exportObjectRule(map[string]*exportJSONRule{
		"type": id, "blocks": identifiers, "fluids": identifiers, "tag": id, "direction": id,
		"state": state, "offset": {items: integer, exactItems: 3},
	})
	predicate.fields["predicate"] = predicate
	predicate.fields["predicates"] = exportArrayRule(predicate)

	count := exportObjectRule(map[string]*exportJSONRule{"type": id})
	count.integers = true
	payload := exportObjectRule(map[string]*exportJSONRule{"source": count, "mean": number, "deviation": number})
	payload.integers = true // ConstantInt and the v2 custom_count fixture.
	exportFields(payload, "min_inclusive max_inclusive", integer)
	count.fields["value"] = payload
	count.fields["distribution"] = exportArrayRule(exportObjectRule(map[string]*exportJSONRule{"data": count, "weight": integer}))

	anchor := exportObjectRule(map[string]*exportJSONRule{})
	anchor.integers = true // v2 also interprets a literal Y coordinate.
	exportFields(anchor, "absolute above_bottom below_top", integer)
	height := exportObjectRule(map[string]*exportJSONRule{"type": id, "value": anchor})
	height.integers = true
	exportFields(height, "absolute above_bottom below_top plateau inner", integer)
	exportFields(height, "min_inclusive max_inclusive", anchor)
	height.fields["distribution"] = exportArrayRule(exportObjectRule(map[string]*exportJSONRule{"data": height, "weight": integer}))

	modifier := exportObjectRule(map[string]*exportJSONRule{
		"type": id, "count": count, "value": count, "height": height, "step": id, "direction_of_search": id,
		"heightmap": {strings: true, validString: regexp.MustCompile(`^[A-Z0-9_]+$`).MatchString},
	})
	exportFields(modifier, "predicate target_condition allowed_search_condition", predicate)
	exportFields(modifier, "chance max_steps max_water_depth min_inclusive max_inclusive below_noise above_noise noise_to_count_ratio", integer)
	exportFields(modifier, "noise_level noise_factor noise_offset", number)
	exportFields(modifier, "xz_spread y_spread", count)
	modifiers := exportArrayRule(modifier)
	placement := exportObjectRule(map[string]*exportJSONRule{
		"count": count, "rarity": integer, "height": height, "min_y": integer, "max_y": integer, "distribution": id,
		"biome_filter": exportBoolRule(), "modifier_types": exportArrayRule(id), "modifiers": modifiers,
	})

	// A partial natural-generation catalog can also contain complete configured
	// carvers. These states select in-game visualization blocks, not log data.
	floatRange := exportObjectRule(map[string]*exportJSONRule{})
	floatRange.numbers = true
	exportFields(floatRange, "min_inclusive max_exclusive min max mean deviation plateau", number)
	scale := exportObjectRule(map[string]*exportJSONRule{"type": id, "value": floatRange})
	scale.numbers = true
	debugSettings := exportObjectRule(map[string]*exportJSONRule{"debug_mode": exportBoolRule()})
	exportFields(debugSettings, "air_state water_state lava_state barrier_state", state)
	// A placed feature references a configured feature by ID or inlines its
	// codec. Only the config object uses the existing configured-feature rules.
	feature := exportObjectRule(map[string]*exportJSONRule{"type": id, "config": exportWorldgenDefinitionJSONRule()})
	feature.strings, feature.validString = true, id.validString
	return naturalGenerationJSONRules{
		placement:     placement,
		placedFeature: exportObjectRule(map[string]*exportJSONRule{"feature": feature, "placement": modifiers}),
		carverFields: map[string]*exportJSONRule{
			"probability": number, "y": height, "yScale": scale, "y_scale": scale, "lava_level": anchor,
			"aquifers_enabled": exportBoolRule(), "replaceable": identifiers, "liquids": identifiers, "debug_settings": debugSettings,
		},
	}
}
