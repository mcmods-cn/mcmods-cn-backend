package database

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuiltinCompatibilityFieldsAreConfigurable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{name: "block", raw: builtinBlockCompatibilityFields},
		{name: "item", raw: builtinItemCompatibilityFields},
		{name: "tool", raw: builtinToolCompatibilityFields},
		{name: "equipment", raw: builtinEquipmentCompatibilityFields},
		{name: "entity", raw: builtinEntityCompatibilityFields},
	}
	schemaSQL := strings.Join(modContentSchemaStatements(), "\n")
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var fields []struct {
				Code          string            `json:"code"`
				Type          string            `json:"type"`
				Names         map[string]string `json:"names"`
				Paths         [][]string        `json:"paths"`
				ReferenceKind string            `json:"referenceKind"`
			}
			if err := json.Unmarshal([]byte(testCase.raw), &fields); err != nil {
				t.Fatalf("decode compatibility fields: %v", err)
			}
			if len(fields) == 0 {
				t.Fatal("compatibility field list is empty")
			}
			seen := make(map[string]struct{}, len(fields))
			for _, field := range fields {
				if field.Code == "" || field.Type == "" {
					t.Fatalf("field is missing code or type: %#v", field)
				}
				if _, exists := seen[field.Code]; exists {
					t.Fatalf("duplicate field code %q", field.Code)
				}
				seen[field.Code] = struct{}{}
				for _, locale := range []string{"en-US", "zh-CN", "zh-TW"} {
					if strings.TrimSpace(field.Names[locale]) == "" {
						t.Errorf("field %q is missing %s name", field.Code, locale)
					}
				}
				if len(field.Paths) == 0 {
					t.Errorf("field %q has no import paths", field.Code)
				}
				if (field.Type == "reference" || field.Type == "reference-list") && field.ReferenceKind == "" {
					t.Errorf("reference field %q has no reference kind", field.Code)
				}
			}
			if !strings.Contains(schemaSQL, testCase.raw) {
				t.Fatal("compatibility fields are not installed into the configurable template schema")
			}
		})
	}
}

func TestNaturalGenerationSizeSupportsNumberProviders(t *testing.T) {
	t.Parallel()

	schemaSQL := strings.Join(modContentCanonicalDocumentSchemaStatements(), "\n")
	if !strings.Contains(schemaSQL, `{"code":"size","type":"json"`) {
		t.Fatal("natural generation size must preserve structured number providers")
	}
	if strings.Contains(schemaSQL, `{"code":"size","type":"number"`) {
		t.Fatal("natural generation size is incorrectly restricted to a scalar number")
	}
}

func TestWorldStructureBiomesSupportsTagOrExplicitList(t *testing.T) {
	t.Parallel()

	schemaSQL := strings.Join(modContentCanonicalDocumentSchemaStatements(), "\n")
	if !strings.Contains(schemaSQL, `{"code":"biomeTag","type":"reference"`) ||
		!strings.Contains(schemaSQL, `{"code":"biomeIds","type":"reference-list"`) {
		t.Fatal("world structure biomes must normalize tag selectors and explicit biome references separately")
	}
	if strings.Contains(schemaSQL, `{"code":"biomes","type":"json"`) {
		t.Fatal("world structure biomes must not remain an uneditable JSON value")
	}
}

func TestCanonicalDocumentSchemasPreserveExporterSpecificFields(t *testing.T) {
	t.Parallel()

	statements := modContentCanonicalDocumentSchemaStatements()
	cases := map[string][]string{
		"advancement": {`"code":"parentId"`, `"code":"childrenIds"`, `"code":"criteria"`, `"code":"requirements"`, `"code":"rewards"`},
		"enchantment": {`"code":"rarityWeight"`, `"code":"anvilCost"`, `"code":"supportedItems"`, `"code":"exclusiveWith"`, `"code":"costs"`},
		"mob_effect":  {`"code":"colorRGB"`, `"code":"effectAttributeModifiers"`},
		"fluid":       {`"code":"bucketItemId"`, `"code":"fluidTags"`, `"code":"amount"`},
		"key_mapping": {`"code":"categoryNames"`, `"code":"defaultKey"`, `"code":"boundKey"`},
	}
	for templateCode, fields := range cases {
		var schemaSQL string
		for _, statement := range statements {
			if strings.Contains(statement, `where code='`+templateCode+`' and builtin`) {
				schemaSQL = statement
				break
			}
		}
		if schemaSQL == "" {
			t.Fatalf("canonical schema for %s is not installed", templateCode)
		}
		for _, field := range fields {
			if !strings.Contains(schemaSQL, field) {
				t.Errorf("canonical schema for %s is missing %s", templateCode, field)
			}
		}
	}
}
