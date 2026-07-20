package httpapi

import "testing"

func TestValidateCatalogResourceDefinitionEditableProperties(t *testing.T) {
	definition := map[string]any{
		"schemaVersion": catalogResourceDefinitionSchema,
		"physical":      map[string]any{"hardness": 3.5, "explosionResistance": 6.0, "lightLevel": 7.0, "requiresCorrectTool": true},
		"tool":          map[string]any{"tier": "minecraft:diamond", "durability": 1561.0, "miningSpeed": 8.0, "attackDamage": 3.0},
		"render":        map[string]any{"mode": "block_model", "modelResourceId": "minecraft:block/stone", "scale": 1.0},
	}
	if err := validateCatalogResourceDefinition("minecraft.block", definition); err != nil {
		t.Fatalf("expected editable block/tool/render properties to be accepted: %v", err)
	}
}

func TestValidateCatalogResourceDefinitionRejectsInvalidKnownProperties(t *testing.T) {
	tests := []map[string]any{
		{"physical": map[string]any{"lightLevel": 16.0}},
		{"physical": map[string]any{"requiresCorrectTool": "yes"}},
		{"tool": map[string]any{"durability": 1.5}},
		{"render": map[string]any{"mode": "unknown_builtin"}},
		{"fluid": map[string]any{"tint": "not-a-color"}},
	}
	for _, definition := range tests {
		if err := validateCatalogResourceDefinition("minecraft.block", definition); err == nil {
			t.Fatalf("expected invalid known definition to be rejected: %#v", definition)
		}
	}
}

func TestValidateCatalogResourceDefinitionAllowsModSpecificProperties(t *testing.T) {
	definition := map[string]any{"modSpecific": map[string]any{"mekanism": map[string]any{"radiation": 4.2}}}
	if err := validateCatalogResourceDefinition("mekanism.chemical", definition); err != nil {
		t.Fatalf("forward-compatible mod-specific properties should be allowed: %v", err)
	}
}
