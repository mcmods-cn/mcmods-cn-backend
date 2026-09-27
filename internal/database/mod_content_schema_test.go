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

func TestImportedResourceProjectionHasScopedOwnership(t *testing.T) {
	t.Parallel()
	if schemaGeneration != 167 {
		t.Fatalf("unexpected schema generation %d", schemaGeneration)
	}
	schemaSQL := strings.Join(modContentSchemaStatements(), "\n")
	for _, required := range []string{
		"projection_source text not null default 'manual'",
		"import_source_namespace text not null default ''",
		"import_source_kind text not null default ''",
		"import_revision_id text not null default ''",
		"idx_mod_resource_version_details_import_scope",
		"where projection_source='import'",
	} {
		if !strings.Contains(schemaSQL, required) {
			t.Errorf("import projection schema is missing %q", required)
		}
	}
}

func TestModContentPlacementSearchProjectionIsIndexedAndTransactional(t *testing.T) {
	t.Parallel()
	schemaSQL := strings.Join(modContentSchemaStatements(), "\n")
	baselineSQL := strings.Join(baselineSchemaStatements(), "\n")
	for _, required := range []string{
		"search_document tsvector not null default ''::tsvector",
		"using gin(search_document)",
		"build_mod_content_resource_search_document",
		"trg_mod_content_search_localization",
		"trg_mod_content_search_import_snapshot",
		"trg_mod_content_search_import_revision_update",
		"trg_mod_content_search_import_revision_delete",
		"trg_mod_content_search_resource",
	} {
		if !strings.Contains(schemaSQL, required) {
			t.Errorf("mod-content search projection is missing %q", required)
		}
	}
	if strings.Contains(baselineSQL, "pg_trgm") {
		t.Fatal("mod-content search projection must not depend on unavailable optional extensions")
	}
}

func TestModContentCategoryDepthIsAnAuthoritativeSubtreeInvariant(t *testing.T) {
	t.Parallel()
	schemaSQL := strings.Join(modContentSchemaStatements(), "\n")
	for _, required := range []string{
		"from mod_content_versions version where version.id=new.version_id for update",
		"with recursive descendants as",
		"parent_depth+descendant_depth>4",
		"content section cannot be its own ancestor",
	} {
		if !strings.Contains(schemaSQL, required) {
			t.Errorf("content category tree invariant is missing %q", required)
		}
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

func TestImportedDetailFieldsAreEditable(t *testing.T) {
	t.Parallel()

	schemaSQL := strings.Join(modContentCanonicalDocumentSchemaStatements(), "\n")
	for _, field := range []string{
		`{"code":"generatorSettings","type":"json"`,
		`{"code":"placement","type":"json"`,
		`{"code":"startHeight","type":"json"`,
	} {
		if !strings.Contains(schemaSQL, field) {
			t.Errorf("canonical schema is missing editable field %s", field)
		}
		if strings.Contains(schemaSQL, field+`,"editable":false`) {
			t.Errorf("canonical schema field %s is unexpectedly read-only", field)
		}
	}
}

func TestCanonicalDocumentFieldsDefaultToEditable(t *testing.T) {
	t.Parallel()

	schemaSQL := strings.Join(modContentCanonicalDocumentSchemaStatements(), "\n")
	if strings.Contains(schemaSQL, `"editable":false`) {
		t.Fatal("built-in resource attribute fields must default to human-editable")
	}
}

func TestLootTableSchemaPreservesEditableDefinitionMetadata(t *testing.T) {
	t.Parallel()

	schemaSQL := strings.Join(modContentCanonicalDocumentSchemaStatements(), "\n")
	for _, field := range []string{`{"code":"tableType","type":"text"`, `{"code":"randomSequence","type":"text"`, `{"code":"pools","type":"json"`} {
		if !strings.Contains(schemaSQL, field) {
			t.Errorf("loot-table schema is missing %s", field)
		}
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
