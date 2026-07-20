package httpapi

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const catalogResourceDefinitionSchema = "mcmods-resource-definition/v1"

// validateCatalogResourceDefinition validates the stable, manually editable
// portion of a game resource. Unknown keys remain allowed so future Minecraft
// versions and mod-specific properties do not require a database migration.
func validateCatalogResourceDefinition(kindCode string, definition map[string]any) error {
	if definition == nil {
		return nil
	}
	encoded, err := json.Marshal(definition)
	if err != nil || len(encoded) > 256*1024 || catalogJSONDepth(definition, 0) > 16 {
		return fmt.Errorf("%w: resource definition is too large or deeply nested", errCatalogEditorInvalid)
	}
	if raw, exists := definition["schemaVersion"]; exists {
		value, ok := raw.(string)
		if !ok || (strings.TrimSpace(value) != "" && strings.TrimSpace(value) != catalogResourceDefinitionSchema) {
			return fmt.Errorf("%w: unsupported resource definition schema", errCatalogEditorInvalid)
		}
	}
	for _, section := range []string{"physical", "tool", "render", "fluid", "chemical"} {
		if raw, exists := definition[section]; exists && raw != nil {
			if _, ok := raw.(map[string]any); !ok {
				return fmt.Errorf("%w: resource definition section %s must be an object", errCatalogEditorInvalid, section)
			}
		}
	}
	if err = validateCatalogPhysicalProperties(catalogObject(definition["physical"])); err != nil {
		return err
	}
	if err = validateCatalogToolProperties(catalogObject(definition["tool"])); err != nil {
		return err
	}
	if err = validateCatalogRenderProperties(catalogObject(definition["render"])); err != nil {
		return err
	}
	if err = validateCatalogFluidProperties(catalogObject(definition["fluid"])); err != nil {
		return err
	}
	if chemical := catalogObject(definition["chemical"]); chemical != nil {
		if err = validateCatalogFluidProperties(chemical); err != nil {
			return err
		}
	}
	if kindCode == "" {
		return fmt.Errorf("%w: resource kind is required", errCatalogEditorInvalid)
	}
	return nil
}

func validateCatalogPhysicalProperties(value map[string]any) error {
	if value == nil {
		return nil
	}
	checks := []catalogNumericField{
		{"hardness", -1, 1e9, false},
		{"explosionResistance", 0, 1e12, false},
		{"friction", 0, 1, false},
		{"speedFactor", 0, 100, false},
		{"jumpFactor", 0, 100, false},
		{"lightLevel", 0, 15, true},
	}
	if err := validateCatalogNumericFields(value, checks); err != nil {
		return err
	}
	return validateCatalogBooleanFields(value, "requiresCorrectTool", "randomTicks", "replaceable", "air")
}

func validateCatalogToolProperties(value map[string]any) error {
	if value == nil {
		return nil
	}
	checks := []catalogNumericField{
		{"durability", 0, math.MaxInt32, true},
		{"miningSpeed", 0, 1e9, false},
		{"attackDamage", -1e6, 1e6, false},
		{"attackSpeed", -1024, 1024, false},
		{"enchantability", 0, 1e6, true},
		{"harvestLevel", -1, 1e6, true},
	}
	if err := validateCatalogNumericFields(value, checks); err != nil {
		return err
	}
	for _, field := range []string{"tier", "toolType", "repairTag"} {
		if raw, exists := value[field]; exists {
			text, ok := raw.(string)
			if !ok || len(text) > 512 {
				return fmt.Errorf("%w: tool.%s is invalid", errCatalogEditorInvalid, field)
			}
		}
	}
	return nil
}

func validateCatalogRenderProperties(value map[string]any) error {
	if value == nil {
		return nil
	}
	if raw, exists := value["mode"]; exists {
		mode, ok := raw.(string)
		if !ok || !catalogStringIn(mode, "icon", "item_model", "block_model", "entity_model", "fluid", "chemical", "custom") {
			return fmt.Errorf("%w: render.mode is invalid", errCatalogEditorInvalid)
		}
	}
	for _, field := range []string{"model", "modelResourceId", "renderer"} {
		if raw, exists := value[field]; exists {
			text, ok := raw.(string)
			if !ok || len(text) > 1024 {
				return fmt.Errorf("%w: render.%s is invalid", errCatalogEditorInvalid, field)
			}
		}
	}
	if err := validateCatalogNumericFields(value, []catalogNumericField{{"scale", 0.001, 1000, false}}); err != nil {
		return err
	}
	for _, field := range []string{"rotation", "translation"} {
		if raw, exists := value[field]; exists {
			items, ok := raw.([]any)
			if !ok || len(items) != 3 {
				return fmt.Errorf("%w: render.%s must contain three numbers", errCatalogEditorInvalid, field)
			}
			for _, item := range items {
				if _, ok = catalogFiniteNumber(item); !ok {
					return fmt.Errorf("%w: render.%s must contain finite numbers", errCatalogEditorInvalid, field)
				}
			}
		}
	}
	return nil
}

func validateCatalogFluidProperties(value map[string]any) error {
	if value == nil {
		return nil
	}
	if err := validateCatalogNumericFields(value, []catalogNumericField{
		{"density", -1e9, 1e9, true},
		{"viscosity", 0, 1e12, true},
		{"temperature", 0, 1e9, true},
		{"luminosity", 0, 15, true},
	}); err != nil {
		return err
	}
	if err := validateCatalogBooleanFields(value, "gaseous"); err != nil {
		return err
	}
	if raw, exists := value["tint"]; exists {
		text, ok := raw.(string)
		if !ok || (!catalogHexColor(text) && strings.TrimSpace(text) != "") {
			return fmt.Errorf("%w: fluid tint is invalid", errCatalogEditorInvalid)
		}
	}
	return nil
}

type catalogNumericField struct {
	name    string
	minimum float64
	maximum float64
	integer bool
}

func validateCatalogNumericFields(value map[string]any, fields []catalogNumericField) error {
	for _, field := range fields {
		raw, exists := value[field.name]
		if !exists || raw == nil {
			continue
		}
		number, ok := catalogFiniteNumber(raw)
		if !ok || number < field.minimum || number > field.maximum || field.integer && math.Trunc(number) != number {
			return fmt.Errorf("%w: numeric property %s is invalid", errCatalogEditorInvalid, field.name)
		}
	}
	return nil
}

func validateCatalogBooleanFields(value map[string]any, fields ...string) error {
	for _, field := range fields {
		if raw, exists := value[field]; exists && raw != nil {
			if _, ok := raw.(bool); !ok {
				return fmt.Errorf("%w: boolean property %s is invalid", errCatalogEditorInvalid, field)
			}
		}
	}
	return nil
}

func catalogFiniteNumber(value any) (float64, bool) {
	var result float64
	switch typed := value.(type) {
	case float64:
		result = typed
	case float32:
		result = float64(typed)
	case int:
		result = float64(typed)
	case int32:
		result = float64(typed)
	case int64:
		result = float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		result = parsed
	default:
		return 0, false
	}
	return result, !math.IsNaN(result) && !math.IsInf(result, 0)
}

func catalogJSONDepth(value any, depth int) int {
	maximum := depth
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			maximum = max(maximum, catalogJSONDepth(child, depth+1))
		}
	case []any:
		for _, child := range typed {
			maximum = max(maximum, catalogJSONDepth(child, depth+1))
		}
	}
	return maximum
}

func catalogObject(value any) map[string]any {
	if value == nil {
		return nil
	}
	result, _ := value.(map[string]any)
	return result
}

func catalogStringIn(value string, allowed ...string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func catalogHexColor(value string) bool {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) != 6 && len(value) != 8 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') && (character < 'A' || character > 'F') {
			return false
		}
	}
	return true
}
