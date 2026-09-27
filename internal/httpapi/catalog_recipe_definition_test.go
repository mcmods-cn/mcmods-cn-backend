package httpapi

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestReconcileCatalogRecipeDefinitionPreservesOpaqueServerValue(t *testing.T) {
	stored := map[string]any{
		"layoutKind": "exporter-specific",
		"nested":     map[string]any{"flags": []any{true, false}, "level": float64(3)},
	}
	matching := map[string]any{
		"nested":     map[string]any{"level": float64(3), "flags": []any{true, false}},
		"layoutKind": "exporter-specific",
	}
	for name, requested := range map[string]map[string]any{"omitted": nil, "matching echo": matching} {
		t.Run(name, func(t *testing.T) {
			got, err := reconcileCatalogRecipeDefinition(requested, stored, true)
			if err != nil || !reflect.DeepEqual(got, stored) {
				t.Fatalf("definition=%#v err=%v", got, err)
			}
		})
	}
	if _, err := reconcileCatalogRecipeDefinition(map[string]any{}, stored, true); !errors.Is(err, errCatalogEditorConflict) {
		t.Fatalf("stale empty echo must fail safely, got %v", err)
	}
	if _, err := reconcileCatalogRecipeDefinition(map[string]any{"injected": true}, nil, false); !errors.Is(err, errCatalogEditorInvalid) {
		t.Fatalf("new recipes must not inject opaque definition data, got %v", err)
	}
	if got, err := reconcileCatalogRecipeDefinition(map[string]any{}, nil, false); err != nil || len(got) != 0 {
		t.Fatalf("empty create definition=%#v err=%v", got, err)
	}
}

func TestCatalogRecipeDefinitionRoundTripIntegration(t *testing.T) {
	db := openGlobalCatalogTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `create temp table recipe_definitions(recipe_id bigint primary key,definition jsonb not null);
		create temp table catalog_entities(id bigint primary key,public_id text not null,status text not null);
		create temp table recipe_layout_templates(entity_id bigint primary key,recipe_type_id bigint not null);
		create temp table recipe_template_slots(template_id bigint not null,slot_key text not null,role text not null);
		insert into recipe_definitions(recipe_id,definition) values(71,'{"layoutKind":"exporter-specific","nested":{"flags":[true,false],"level":3}}');
		insert into catalog_entities(id,public_id,status) values(51,'tpl234567','active');
		insert into recipe_layout_templates(entity_id,recipe_type_id) values(51,10);
		insert into recipe_template_slots(template_id,slot_key,role) values(51,'output','output')`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: db}
	requested := map[string]any{
		"layoutKind": "exporter-specific",
		"nested":     map[string]any{"flags": []any{true, false}, "level": float64(3)},
	}
	edit := catalogRecipeEdit{
		TemplatePublicID: "tpl234567",
		Definition:       requested,
		Bindings: map[string]catalogRecipeBindingEdit{
			"output": {Candidates: []catalogRecipeCandidateEdit{{ResourcePublicID: "res234567", Amount: 1}}},
		},
	}
	if err := server.normalizeCatalogRecipeEdit(ctx, 10, 71, &edit); err != nil || !reflect.DeepEqual(edit.Definition, requested) {
		t.Fatalf("normalized definition=%#v err=%v", edit.Definition, err)
	}
	if _, err := server.catalogRecipeDefinitionForMutation(ctx, 71, map[string]any{}); !errors.Is(err, errCatalogEditorConflict) {
		t.Fatalf("stale empty echo must fail safely, got %v", err)
	}
}
