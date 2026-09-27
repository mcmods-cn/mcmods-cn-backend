package httpapi

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRecipeRenderRejectsHistoricalNonObjectJSONIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify recipe render JSON integrity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `
		create temporary table recipe_import_snapshots(
			id text primary key,layout_available boolean not null,layout_kind text not null,ordered boolean,
			layout_classification_source text not null,width integer,height integer,parameters jsonb not null,
			origin_kind text not null,underlying_recipe_type_id text not null,source_mod_id text not null,
			source_mod_version text not null,source_mod_id_source text not null,render_locale text not null,template_id text
		);
		create temporary table recipe_template_import_snapshots(
			id text primary key,source_template_id text,background_path text,background_contains_ingredients boolean,
			coordinate_space text,image_scale integer,canvas jsonb,image_pixels jsonb,content_rect jsonb
		);
		create temporary table recipe_template_import_slots(
			id text primary key,template_id text,source_slot_id text,role text,ordinal integer,
			coordinates_available boolean,rect jsonb,visual_rect jsonb,data jsonb
		);
		create temporary table recipe_import_bindings(
			id text primary key,recipe_snapshot_id text,template_slot_id text,ingredient_present boolean,
			clickable boolean,placeholder_item text,item_tag_equivalent text,semantic_role text,role_source text,tag_id bigint
		);
		create temporary table catalog_tags(entity_id bigint primary key,registry text,canonical_id text);
		create temporary table catalog_entities(id bigint primary key,public_id text);
		create temporary table recipe_import_binding_candidates(
			binding_id text,alternative_index integer,raw_resource_id text,amount double precision,
			ingredient_kind text,ingredient_type text,unique_id text,nbt_snbt text,chance_available boolean,
			chance double precision,chance_percent double precision,chance_comparator text,chance_source text,
			chance_text text,chance_texts jsonb,chance_translation_key text,chance_render_x double precision,
			chance_render_y double precision,byproduct boolean
		);
		insert into recipe_import_snapshots values(
			'arch032',true,'shaped',true,'fixture',1,1,'{}'::jsonb,
			'jei','minecraft:crafting','minecraft','1.21','recipe','en_us',null
		);
		insert into recipe_template_import_snapshots values(
			'arch032-template','crafting','','false','logical_pixels',1,
			'{"width":1,"height":1}'::jsonb,'{}'::jsonb,'{}'::jsonb
		);
		insert into recipe_template_import_slots values(
			'arch032-slot','arch032-template','input-0','input',0,true,
			'{"x":0,"y":0,"width":1,"height":1}'::jsonb,
			'{"x":0,"y":0,"width":1,"height":1}'::jsonb,'{}'::jsonb
		);
		insert into recipe_import_bindings values(
			'arch032-binding','arch032','arch032-slot',true,true,'','','input','fixture',null
		);
		insert into recipe_import_binding_candidates values(
			'arch032-binding',0,'minecraft:stone',1,'item','item','minecraft:stone','',
			false,null,null,'','','','{}'::jsonb,'',null,null,false
		)
	`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	for _, value := range []string{"null", "[]", `"text"`, "1"} {
		if _, err = pool.Exec(ctx, `update recipe_import_snapshots set parameters=$1::jsonb where id='arch032'`, value); err != nil {
			t.Fatal(err)
		}
		recipes := []map[string]any{{"recipeSnapshotId": "arch032"}}
		if err = server.hydrateRecipeRenderLayouts(ctx, recipes); err == nil {
			t.Fatalf("historical parameters %s rendered as %#v", value, recipes[0]["layout"])
		}
	}
	if _, err = pool.Exec(ctx, `update recipe_import_snapshots set parameters='{"group":"building"}'::jsonb,template_id='arch032-template' where id='arch032'`); err != nil {
		t.Fatal(err)
	}

	objectColumns := []struct {
		table  string
		column string
		where  string
		valid  string
	}{
		{"recipe_template_import_snapshots", "canvas", "id='arch032-template'", `{"width":1,"height":1}`},
		{"recipe_template_import_snapshots", "image_pixels", "id='arch032-template'", `{}`},
		{"recipe_template_import_snapshots", "content_rect", "id='arch032-template'", `{}`},
		{"recipe_template_import_slots", "data", "id='arch032-slot'", `{}`},
		{"recipe_template_import_slots", "rect", "id='arch032-slot'", `{"x":0,"y":0,"width":1,"height":1}`},
		{"recipe_template_import_slots", "visual_rect", "id='arch032-slot'", `{"x":0,"y":0,"width":1,"height":1}`},
		{"recipe_import_binding_candidates", "chance_texts", "binding_id='arch032-binding'", `{}`},
	}
	for _, field := range objectColumns {
		for _, value := range []string{"null", "[]", `"text"`, "1"} {
			statement := fmt.Sprintf("update %s set %s=$1::jsonb where %s", field.table, field.column, field.where)
			if _, err = pool.Exec(ctx, statement, value); err != nil {
				t.Fatal(err)
			}
			recipes := []map[string]any{{"recipeSnapshotId": "arch032"}}
			if err = server.hydrateRecipeRenderLayouts(ctx, recipes); err == nil {
				t.Fatalf("historical %s.%s %s rendered as %#v", field.table, field.column, value, recipes[0]["layout"])
			}
		}
		statement := fmt.Sprintf("update %s set %s=$1::jsonb where %s", field.table, field.column, field.where)
		if _, err = pool.Exec(ctx, statement, field.valid); err != nil {
			t.Fatal(err)
		}
	}

	recipes := []map[string]any{{"recipeSnapshotId": "arch032"}}
	if err = server.hydrateRecipeRenderLayouts(ctx, recipes); err != nil {
		t.Fatalf("healthy recipe layout failed: %v", err)
	}
	layout, _ := recipes[0]["layout"].(map[string]any)
	parameters, _ := layout["parameters"].(map[string]any)
	if parameters["group"] != "building" {
		t.Fatalf("healthy parameters = %#v", parameters)
	}
	slots, _ := layout["slots"].([]any)
	if len(slots) != 1 {
		t.Fatalf("healthy slots = %#v", slots)
	}
	slot, _ := slots[0].(map[string]any)
	alternatives, _ := slot["alternatives"].([]any)
	if len(alternatives) != 1 {
		t.Fatalf("healthy alternatives = %#v", alternatives)
	}
}
