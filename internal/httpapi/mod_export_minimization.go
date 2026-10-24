package httpapi

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
)

// Preserve encoded pixels, alpha, palette and color interpretation. Text,
// EXIF, timestamps and unregistered ancillary chunks have no catalog purpose.
func minimizeExportPNG(data []byte) ([]byte, error) {
	signature := []byte{137, 80, 78, 71, 13, 10, 26, 10}
	if len(data) < 8 || !bytes.Equal(data[:8], signature) {
		return nil, errors.New("invalid PNG signature")
	}
	result := append([]byte(nil), signature...)
	for offset := 8; offset < len(data); {
		if len(data)-offset < 12 {
			return nil, errors.New("truncated PNG chunk")
		}
		n := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
		if n > uint64(len(data)-offset-12) {
			return nil, errors.New("truncated PNG data")
		}
		end := offset + 12 + int(n)
		switch string(data[offset+4 : offset+8]) {
		case "iCCP":
			payload, err := projectExportICCChunk(data[offset+8 : end-4])
			if err != nil {
				return nil, err
			}
			chunk := make([]byte, len(payload)+12)
			binary.BigEndian.PutUint32(chunk[:4], uint32(len(payload)))
			copy(chunk[4:8], "iCCP")
			copy(chunk[8:], payload)
			binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
			result = append(result, chunk...)
		case "IHDR", "PLTE", "IDAT", "IEND", "tRNS", "gAMA", "cHRM", "sRGB", "sBIT":
			result = append(result, data[offset:end]...)
		default:
			if data[offset+4]&0x20 == 0 {
				return nil, errors.New("unsupported critical PNG chunk")
			}
		}
		if string(data[offset+4:offset+8]) == "IEND" {
			if end != len(data) {
				return nil, errors.New("trailing PNG data")
			}
			return result, nil
		}
		offset = end
	}
	return nil, errors.New("missing PNG end chunk")
}

func minimizeExportManifest(manifest *modExportManifest) {
	if len(manifest.Errors) > 128 {
		manifest.Errors = manifest.Errors[:128]
	}
	for index := range manifest.Errors {
		var diagnostic struct {
			Stage string `json:"stage"`
		}
		_ = json.Unmarshal(manifest.Errors[index], &diagnostic)
		// Stages use identifiers, never machine paths or arbitrary exception text.
		if !registeredExportDiagnosticStage(diagnostic.Stage) {
			diagnostic.Stage = "unknown"
		}
		manifest.Errors[index], _ = json.Marshal(map[string]string{"stage": diagnostic.Stage, "code": "export_stage_error"})
	}
}

func registeredExportDiagnosticStage(stage string) bool {
	switch stage {
	case "registry", "registries", "registry_data", "languages", "compatibility", "content_associations", "associations", "trades", "tags", "recipes", "advancements", "worldgen", "worldgen_data", "resources", "block_entity_models", "entity_models", "item_icons", "block_icons", "mob_effect_icons", "effect_icons", "jei", "jei_ingredients", "jei_recipe_gui", "entity_renders":
		return true
	}
	return false
}

func minimizeExportLocaleText(raw json.RawMessage) json.RawMessage {
	var values map[string]string
	if json.Unmarshal(raw, &values) != nil {
		return json.RawMessage(`{}`)
	}
	result := make(map[string]string)
	for locale, value := range values {
		if canonical, ok := exportContentLocale(locale); ok {
			result[canonical] = value
		}
	}
	encoded, _ := json.Marshal(result)
	return encoded
}

// Display has a defined Minecraft/editor purpose; unknown objects nested into
// it cannot inherit the public JSON-field permission.
func minimizeAdvancementDisplay(value any) any {
	source, ok := value.(map[string]any)
	if !ok {
		return value
	}
	result := make(map[string]any)
	for _, key := range []string{"frame", "background", "show_toast", "announce_chat", "announce_to_chat", "hidden", "x", "y", "width", "height", "title_translation_key", "description_translation_key"} {
		if v, exists := source[key]; exists {
			switch v.(type) {
			case string, bool, float64, int:
				result[key] = v
			}
		}
	}
	for _, key := range []string{"title_names", "description_names"} {
		if raw, err := json.Marshal(source[key]); err == nil && source[key] != nil {
			result[key] = json.RawMessage(minimizeExportLocaleText(raw))
		}
	}
	for _, key := range []string{"title", "description"} {
		if v, exists := source[key]; exists {
			result[key] = minimizeMinecraftTextComponent(v)
		}
	}
	if icon, ok := source["icon"].(map[string]any); ok {
		projected := map[string]any{}
		for _, key := range []string{"item", "id", "count", "nbt"} {
			if v, exists := icon[key]; exists {
				switch v.(type) {
				case string, float64:
					projected[key] = v
				}
			}
		}
		result["icon"] = projected
	}
	return result
}
func minimizeMinecraftTextComponent(value any) any {
	switch source := value.(type) {
	case string:
		return source
	case []any:
		result := make([]any, len(source))
		for i, item := range source {
			result[i] = minimizeMinecraftTextComponent(item)
		}
		return result
	case map[string]any:
		result := map[string]any{}
		for _, key := range []string{"text", "translate", "fallback", "color", "bold", "italic", "underlined", "strikethrough", "obfuscated", "font", "insertion"} {
			if v, exists := source[key]; exists {
				switch v.(type) {
				case string, bool:
					result[key] = v
				}
			}
		}
		for _, key := range []string{"with", "extra"} {
			if v, exists := source[key]; exists {
				result[key] = minimizeMinecraftTextComponent(v)
			}
		}
		return result
	default:
		return nil
	}
}
