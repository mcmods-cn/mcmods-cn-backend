package httpapi

import (
	"errors"
	"math"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

// This registry describes the purpose of nested values, rather than inheriting
// a top-level JSON permission. Unknown business fields fail explicitly. Only
// recognized unrelated diagnostic/identity metadata is removed.
type exportJSONRule struct {
	fields      map[string]*exportJSONRule
	values      *exportJSONRule
	items       *exportJSONRule
	exactItems  int
	strings     bool
	numbers     bool
	integers    bool
	booleans    bool
	key         func(string) bool
	validString func(string) bool
}

var errExportJSONPurpose = errors.New("unregistered JSON purpose")

func exportPrivateMetadataKey(key string) bool {
	switch strings.ToLower(strings.ReplaceAll(key, "-", "_")) {
	case "machine_path", "access_token", "refresh_token", "session_token", "authorization", "password", "client_secret", "secret_key", "api_key", "home_directory", "working_directory", "player_uuid", "player_name", "username", "user_uuid", "environment", "stack_trace", "stacktrace", "exception", "debug", "diagnostics":
		return true
	}
	return false
}

func (rule *exportJSONRule) project(value any) (any, error) {
	return rule.projectAtDepth(value, 0)
}
func (rule *exportJSONRule) projectAtDepth(value any, depth int) (any, error) {
	if depth > 24 {
		return nil, errExportJSONPurpose
	}
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case string:
		if rule.strings && (rule.validString == nil || rule.validString(typed)) {
			return typed, nil
		}
	case float64:
		if (rule.numbers || (rule.integers && math.Trunc(typed) == typed)) && !math.IsNaN(typed) && !math.IsInf(typed, 0) {
			return typed, nil
		}
	case bool:
		if rule.booleans {
			return typed, nil
		}
	case []any:
		if rule.items == nil || (rule.exactItems > 0 && len(typed) != rule.exactItems) {
			break
		}
		result := make([]any, len(typed))
		for index, item := range typed {
			projected, err := rule.items.projectAtDepth(item, depth+1)
			if err != nil {
				return nil, err
			}
			result[index] = projected
		}
		return result, nil
	case map[string]any:
		if rule.fields == nil && rule.values == nil {
			break
		}
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			if exportPrivateMetadataKey(key) {
				continue
			}
			child := rule.fields[key]
			if child == nil && rule.values != nil && (rule.key == nil || rule.key(key)) {
				child = rule.values
			}
			if child == nil {
				return nil, errExportJSONPurpose
			}
			projected, err := child.projectAtDepth(item, depth+1)
			if err != nil {
				return nil, err
			}
			result[key] = projected
		}
		return result, nil
	}
	return nil, errExportJSONPurpose
}

func exportObjectRule(fields map[string]*exportJSONRule) *exportJSONRule {
	return &exportJSONRule{fields: fields}
}
func exportArrayRule(items *exportJSONRule) *exportJSONRule { return &exportJSONRule{items: items} }
func exportStringRule() *exportJSONRule                     { return &exportJSONRule{strings: true} }
func exportNumberRule() *exportJSONRule                     { return &exportJSONRule{numbers: true} }
func exportBoolRule() *exportJSONRule                       { return &exportJSONRule{booleans: true} }
func exportScalarRule() *exportJSONRule {
	return &exportJSONRule{strings: true, numbers: true, booleans: true}
}
func exportMapRule(values *exportJSONRule) *exportJSONRule { return &exportJSONRule{values: values} }
func exportFields(rule *exportJSONRule, keys string, child *exportJSONRule) {
	for _, key := range strings.Fields(keys) {
		rule.fields[key] = child
	}
}

func exportResourceReferenceRule() *exportJSONRule {
	return &exportJSONRule{strings: true, validString: func(value string) bool {
		if value == "" {
			return true
		}
		if strings.HasPrefix(value, "#") {
			value = strings.TrimPrefix(value, "#")
		}
		if strings.HasPrefix(value, "/") || strings.Contains(value, "..") {
			return false
		}
		return strings.IndexFunc(value, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("_:./-", r))
		}) < 0
	}}
}

func exportTextJSONRule() *exportJSONRule {
	rule := exportObjectRule(map[string]*exportJSONRule{})
	rule.strings = true
	rule.items = rule
	exportFields(rule, "text translate fallback color font insertion", exportStringRule())
	exportFields(rule, "bold italic underlined strikethrough obfuscated", exportBoolRule())
	exportFields(rule, "with extra", exportArrayRule(rule))
	return rule
}

func exportDisplayJSONRule() *exportJSONRule {
	rule := exportObjectRule(map[string]*exportJSONRule{
		"title": exportTextJSONRule(), "description": exportTextJSONRule(),
		"title_names": exportMapRule(exportStringRule()), "description_names": exportMapRule(exportStringRule()),
		"icon": exportObjectRule(map[string]*exportJSONRule{"item": exportStringRule(), "id": exportStringRule(), "nbt": exportStringRule(), "count": exportNumberRule()}),
	})
	exportFields(rule, "frame background title_translation_key description_translation_key", exportStringRule())
	exportFields(rule, "show_toast announce_chat announce_to_chat hidden", exportBoolRule())
	exportFields(rule, "x y width height", exportNumberRule())
	return rule
}

func exportAdvancementJSONRule(normalized bool) *exportJSONRule {
	rule := exportObjectRule(map[string]*exportJSONRule{
		"display":      exportDisplayJSONRule(),
		"requirements": exportArrayRule(exportArrayRule(exportStringRule())),
		"rewards":      exportObjectRule(map[string]*exportJSONRule{"experience": exportNumberRule(), "loot": exportArrayRule(exportStringRule()), "recipes": exportArrayRule(exportStringRule()), "function": exportStringRule()}),
	})
	exportFields(rule, "id namespace path parent normalization_status raw_definition_path", exportStringRule())
	exportFields(rule, "children", exportArrayRule(exportStringRule()))
	exportFields(rule, "max_criteria_required", exportNumberRule())
	if normalized {
		rule.fields["criteria"] = exportArrayRule(exportStringRule())
	} else {
		// Trigger-specific conditions are business extensions requiring a typed
		// adapter. The standard impossible trigger has no conditions to retain.
		rule.fields["criteria"] = exportMapRule(exportObjectRule(map[string]*exportJSONRule{"trigger": exportStringRule(), "conditions": exportObjectRule(map[string]*exportJSONRule{})}))
	}
	return rule
}

func exportModelJSONRule() *exportJSONRule {
	number := exportNumberRule()
	vector := exportArrayRule(number)
	transform := exportObjectRule(map[string]*exportJSONRule{"rotation": vector, "translation": vector, "scale": vector})
	face := exportObjectRule(map[string]*exportJSONRule{"uv": vector, "texture": exportStringRule(), "cullface": exportStringRule(), "rotation": number, "tintindex": number})
	element := exportObjectRule(map[string]*exportJSONRule{
		"from": vector, "to": vector, "shade": exportBoolRule(),
		"rotation": exportObjectRule(map[string]*exportJSONRule{"origin": vector, "axis": exportStringRule(), "angle": number, "rescale": exportBoolRule()}),
		"faces":    exportObjectRule(map[string]*exportJSONRule{"down": face, "up": face, "north": face, "south": face, "west": face, "east": face}),
	})
	display := exportObjectRule(map[string]*exportJSONRule{})
	exportFields(display, "thirdperson_righthand thirdperson_lefthand firstperson_righthand firstperson_lefthand gui ground fixed head", transform)
	rule := exportObjectRule(map[string]*exportJSONRule{
		"textures": exportMapRule(exportResourceReferenceRule()), "display": display, "elements": exportArrayRule(element),
		"overrides": exportArrayRule(exportObjectRule(map[string]*exportJSONRule{"predicate": exportMapRule(number), "model": exportStringRule()})),
		"transform": transform,
	})
	exportFields(rule, "parent gui_light loader model render_type render_types", exportStringRule())
	exportFields(rule, "parent loader model render_type", exportResourceReferenceRule())
	exportFields(rule, "ambientocclusion flip_v detectCullableFaces diffuseLighting automaticCulling shadeQuads", exportBoolRule())
	// Forge composite models recursively use the same model contract.
	rule.fields["children"] = exportMapRule(rule)
	return rule
}

func exportBlockstateJSONRule() *exportJSONRule {
	model := exportObjectRule(map[string]*exportJSONRule{"model": exportStringRule(), "x": exportNumberRule(), "y": exportNumberRule(), "uvlock": exportBoolRule(), "weight": exportNumberRule()})
	model.items = model
	condition := exportMapRule(exportScalarRule())
	condition.fields = map[string]*exportJSONRule{"OR": exportArrayRule(condition), "AND": exportArrayRule(condition)}
	return exportObjectRule(map[string]*exportJSONRule{
		"variants":  exportMapRule(model),
		"multipart": exportArrayRule(exportObjectRule(map[string]*exportJSONRule{"when": condition, "apply": model})),
	})
}

func exportTextureJSONRule() *exportJSONRule {
	frame := exportObjectRule(map[string]*exportJSONRule{"index": exportNumberRule(), "time": exportNumberRule()})
	frame.numbers = true
	return exportObjectRule(map[string]*exportJSONRule{
		"animation": exportObjectRule(map[string]*exportJSONRule{"frametime": exportNumberRule(), "frames": exportArrayRule(frame), "width": exportNumberRule(), "height": exportNumberRule(), "interpolate": exportBoolRule()}),
		"texture":   exportObjectRule(map[string]*exportJSONRule{"blur": exportBoolRule(), "clamp": exportBoolRule(), "mipmaps": exportArrayRule(exportNumberRule())}),
	})
}

func exportBlockEntityIndexJSONRule() *exportJSONRule {
	texture := exportObjectRule(map[string]*exportJSONRule{})
	exportFields(texture, "appearance texture_id resource_id path source atlas_id", exportStringRule())
	exportFields(texture, "width height", exportNumberRule())
	texture.fields["uv_transform_required"] = exportBoolRule()
	variant := exportObjectRule(map[string]*exportJSONRule{"textures": exportArrayRule(texture), "uv_complete": exportBoolRule()})
	exportFields(variant, "variant_id obj mesh uv_space", exportStringRule())
	exportFields(variant, "vertex_count quad_count texture_count", exportNumberRule())
	model := exportObjectRule(map[string]*exportJSONRule{"variants": exportArrayRule(variant), "model_available": exportBoolRule(), "variant_count": exportNumberRule()})
	exportFields(model, "block_id block_entity_type_id model_source", exportStringRule())
	return exportObjectRule(map[string]*exportJSONRule{"schema_version": exportStringRule(), "coordinate_space": exportStringRule(), "uv_representation": exportStringRule(), "count": exportNumberRule(), "block_entities": exportArrayRule(model)})
}
func exportBlockEntityMeshJSONRule() *exportJSONRule {
	vector := exportArrayRule(exportNumberRule())
	vertex := exportObjectRule(map[string]*exportJSONRule{"position": vector, "uv": vector, "normal": vector})
	return exportObjectRule(map[string]*exportJSONRule{"schema_version": exportStringRule(), "coordinate_space": exportStringRule(), "uv_space": exportStringRule(), "uv_origin": exportStringRule(), "vertex_count": exportNumberRule(), "quad_count": exportNumberRule(), "faces": exportArrayRule(exportObjectRule(map[string]*exportJSONRule{"vertices": exportArrayRule(vertex)}))})
}

// Registered vanilla worldgen vocabulary is recursive because vanilla codecs
// nest configured features, providers, states and placement modifiers. Unknown
// mod codec fields require registration instead of inheriting public access.
func exportWorldgenDefinitionJSONRule() *exportJSONRule {
	rule := exportObjectRule(map[string]*exportJSONRule{})
	rule.strings, rule.numbers, rule.booleans, rule.items = true, true, true, rule
	exportFields(rule, "type feature config features default count height distribution min_inclusive max_inclusive value anchor absolute above_bottom below_top size discard_chance_on_air_exposure targets target state predicate placement chance rarity offset min max spread_xz spread_y noise_level noise_to_count_ratio noise_offset min_water_depth max_water_depth surface biome block block_id blocks fluids fluid layers radius half_height replaceable heightmap direction allowed_search_condition disallowed_search_condition search_range random_name predicate_type tag min_y max_y biome_filter modifier_types modifiers kind id source biome_tag biome_ids dimension_id settings sea_level disable_mob_generation aquifers_enabled ore_veins_enabled legacy_random_source default_block default_fluid noise_router surface_rule spawn_target noise min_y height size_horizontal size_vertical firstOctave amplitudes shift_x shift_y shift_z xz_scale y_scale noise terrain_shaper final_density continents erosion depth ridges temperature vegetation router type block_states structures lakes biome_source biomes preset parameters continentalness weirdness humidity offset altitude", rule)
	rule.fields["Name"] = exportStringRule()
	rule.fields["Properties"] = exportMapRule(exportStringRule())
	return rule
}

func exportPartialCatalogJSONRule(name string) *exportJSONRule {
	rule := exportObjectRule(map[string]*exportJSONRule{})
	exportFields(rule, "schema_version source export_mode filter_mode", exportStringRule())
	exportFields(rule, "count entry_count language_count filtered_recipe_unlock_count filtered_no_display_count filtered_count placed_feature_count configured_carver_count complete_count partial_count consumed_biome_modifier_count unreferenced_configured_feature_count technical_registry_category_count", exportNumberRule())
	exportFields(rule, "roots technical_registry_categories_omitted_from_catalog", exportArrayRule(exportStringRule()))
	if name == "advancements/advancements.json" {
		rule.fields["advancements"] = exportArrayRule(exportAdvancementJSONRule(true))
		return rule
	}
	if name == "worldgen/natural_generation.json" {
		entry := exportObjectRule(map[string]*exportJSONRule{})
		exportFields(entry, "id namespace path generation_id entry_kind source_mod_id source_mod_version category normalization_status dimension_resolution feature_type carver_type configured_feature_id name_source_block_id definition_path placed_feature_definition_path configured_feature_definition_path", exportStringRule())
		exportFields(entry, "runtime_registered", exportBoolRule())
		exportFields(entry, "generation_steps resolved_biome_ids biome_modifier_ids dimension_ids configured_feature_ids referenced_feature_ids", exportArrayRule(exportStringRule()))
		entry.fields["names"] = exportMapRule(exportStringRule())
		entry.fields["configured_feature_definition_paths"] = exportArrayRule(exportObjectRule(map[string]*exportJSONRule{"configured_feature_id": exportStringRule(), "path": exportStringRule()}))
		entry.fields["translation_key"] = exportStringRule()
		entry.fields["discard_chance_on_air_exposure"] = exportNumberRule()
		exportFields(entry, "outputs targets size biome_selectors", exportWorldgenDefinitionJSONRule())
		entry.fields["generation_step"] = exportResourceReferenceRule()
		entry.fields["placement"] = naturalGenerationRules.placement
		for key, field := range naturalGenerationRules.carverFields {
			entry.fields[key] = field
		}
		rule.fields["entries"] = exportArrayRule(entry)
		rule.fields["counts_by_category"] = exportMapRule(exportNumberRule())
		return rule
	}
	return nil
}

func projectExportWavefront(name string, data []byte, purpose exportAssetPurpose) ([]byte, error) {
	if !utf8.Valid(data) {
		return nil, exportAssetRegistrationError(purpose, name)
	}
	lines := strings.Split(string(data), "\n")
	var result strings.Builder
	for _, line := range lines {
		line, _, _ = strings.Cut(line, "#")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		command := fields[0]
		valid := false
		if purpose == exportAssetOBJ {
			switch command {
			case "v", "vt", "vn", "vp":
				valid = len(fields) >= 2 && len(fields) <= 8
				for _, field := range fields[1:] {
					number, err := strconv.ParseFloat(field, 64)
					valid = valid && err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
				}
			case "f", "l", "p":
				valid = len(fields) >= 2
				for _, field := range fields[1:] {
					for _, index := range strings.Split(field, "/") {
						if index != "" {
							_, err := strconv.ParseInt(index, 10, 64)
							valid = valid && err == nil
						}
					}
				}
			case "o", "g", "usemtl", "s":
				valid = len(fields) >= 2
				for _, field := range fields[1:] {
					valid = valid && exportWavefrontName(field)
				}
			case "mtllib":
				valid = len(fields) >= 2
				for _, field := range fields[1:] {
					valid = valid && exportWavefrontReference(name, field, ".mtl")
				}
			}
		} else {
			switch command {
			case "newmtl":
				valid = len(fields) == 2 && exportWavefrontName(fields[1])
			case "Ka", "Kd", "Ks", "Ke", "Tf", "Ns", "Ni", "d", "Tr", "illum":
				valid = len(fields) >= 2 && len(fields) <= 4
				for _, field := range fields[1:] {
					number, err := strconv.ParseFloat(field, 64)
					valid = valid && err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
				}
			case "map_Kd", "map_Ka", "map_Ks", "map_Ke", "map_d", "bump", "map_Bump", "norm":
				// Option-bearing material texture codecs require an explicit adapter.
				valid = len(fields) == 2 && exportWavefrontReference(name, fields[1], ".png")
			}
		}
		if !valid {
			return nil, exportAssetRegistrationError(purpose, name)
		}
		result.WriteString(strings.Join(fields, " "))
		result.WriteByte('\n')
	}
	return []byte(result.String()), nil
}

func exportWavefrontName(value string) bool {
	return value != "" && strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_-.:", r))
	}) < 0
}
func exportWavefrontReference(source, value, suffix string) bool {
	if strings.ContainsAny(value, "\\:\x00") || strings.HasPrefix(value, "/") || !strings.HasSuffix(strings.ToLower(value), suffix) {
		return false
	}
	resolved := path.Clean(path.Join(path.Dir(source), value))
	// Relative references may use ../ inside the same namespace, but may not
	// escape its asset root or acquire a local/remote filesystem meaning.
	parts := strings.SplitN(source, "/", 3)
	return len(parts) == 3 && parts[0] == "assets" && strings.HasPrefix(resolved, "assets/"+parts[1]+"/")
}
