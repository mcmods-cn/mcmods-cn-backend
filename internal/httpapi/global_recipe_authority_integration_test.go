package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInvalidImportedCatalystsAreNotSilentlyEmpty(t *testing.T) {
	server := &Server{}
	for _, raw := range []string{`{"not":"an array"}`, `[1]`, `null`, ``} {
		if _, err := server.decorateCatalysts(context.Background(), []byte(raw), "revision-test"); err == nil || !strings.Contains(err.Error(), "revision-test") {
			t.Fatalf("invalid catalysts %q returned %v", raw, err)
		}
	}
}

func TestGlobalRecipeTypeListUsesBatchedCatalystQueries(t *testing.T) {
	globalSource, err := os.ReadFile("global_catalog_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(globalSource)
	start := strings.Index(text, "func (s *Server) globalRecipeTypes(")
	end := strings.Index(text, "func (s *Server) globalRecipeTypeCatalog(")
	if start < 0 || end <= start {
		t.Fatal("global recipe type list boundary was not found")
	}
	listSource := text[start:end]
	if strings.Contains(listSource, "catalogRecipeTypeCatalysts(ctx, entityID") || !strings.Contains(listSource, "catalogRecipeTypesCatalysts(ctx, typeIDs") {
		t.Fatal("global recipe type list does not batch catalyst loading for the page")
	}
	editorSource, err := os.ReadFile("catalog_recipe_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	editorText := string(editorSource)
	batchStart := strings.Index(editorText, "func (s *Server) catalogRecipeTypesCatalysts(")
	batchEnd := strings.Index(editorText, "func (s *Server) catalogTemplateSlotRows(")
	if batchStart < 0 || batchEnd <= batchStart {
		t.Fatal("batched catalyst loader boundary was not found")
	}
	batchSource := editorText[batchStart:batchEnd]
	if strings.Count(batchSource, "=any($1::bigint[])") != 2 || !strings.Contains(batchSource, "decorateCatalystBatches(ctx, batches)") {
		t.Fatal("catalyst loader does not use one canonical and one fallback page query")
	}
}

func TestPublicRecipeSelectionPrefersCanonicalAndKeepsImportFallbackIntegration(t *testing.T) {
	db := openGlobalCatalogTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	createGlobalRecipeAuthorityTestTables(t, ctx, db)
	if _, err := db.Exec(ctx, `insert into mods(id,slug) values(50,'source-mod');
		insert into catalog_entities(id,public_id,status,default_locale) values
			(1,'type00001','active','en-US'),(2,'manual001','active','en-US'),(3,'import001','active','en-US'),
			(4,'resource1','active','en-US'),(5,'template1','active','en-US');
		insert into recipe_types(entity_id,canonical_id) values(1,'minecraft:crafting');
		insert into recipes(entity_id,recipe_type_id,canonical_source_id,semantic_fingerprint,owner_mod_id,identity_source)
			values(2,1,null,'manual-fingerprint',null,'manual'),(3,1,null,'import-fingerprint',50,'jei_category');
		insert into content_revisions(id,public_id) values(70,'revision70');
		insert into recipe_layout_templates(entity_id,recipe_type_id,template_key,canvas_width,canvas_height,image_scale)
			values(5,1,'crafting-grid',176,88,1);
		insert into recipe_template_slots(id,template_id,slot_key,role,ordinal,x,y,width,height,definition)
			values(10,5,'input-0','input',0,12,20,16,16,'{}');
		insert into recipe_definitions(recipe_id,template_id,definition,published_revision_id)
			values(2,5,'{"layout_kind":"shaped"}',70);
		insert into recipe_bindings(id,recipe_id,template_slot_id,ordinal,definition) values(20,2,10,0,'{}');
		insert into game_resources(entity_id,kind_code,canonical_id,namespace) values(4,'minecraft.item','minecraft:stone','minecraft');
		insert into content_localizations(catalog_entity_id,locale,name) values(4,'en-US','Stone'),(4,'zh-CN','石头');
		insert into recipe_binding_candidates(binding_id,candidate_index,resource_id,amount,probability,byproduct,definition)
			values(20,0,4,2,0.5,false,'{}');
		insert into catalog_import_revisions(id,mod_id,package_id,is_active,status,activated_at,created_at)
			values('import-revision',50,'package-1',true,'ready',now(),now());
		insert into recipe_import_snapshots(id,recipe_id,revision_id,source_recipe_id,source_id_kind)
			values('manual-observation',2,'import-revision','observed-manual','exported'),
			('import-observation',3,'import-revision','observed-import','exported')`); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query(ctx, `with `+publicRecipeSelectionCTE+`
		select entity_id,recipe_id,snapshot_id,source_revision_id,authoritative
		from public_recipes order by entity_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type selected struct {
		entityID                               int64
		recipeID, snapshotID, sourceRevisionID string
		authoritative                          bool
	}
	items := make([]selected, 0, 2)
	for rows.Next() {
		var item selected
		if err = rows.Scan(&item.entityID, &item.recipeID, &item.snapshotID, &item.sourceRevisionID, &item.authoritative); err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].entityID != 2 || !items[0].authoritative || items[0].snapshotID != "" || items[0].sourceRevisionID != "revision70" ||
		items[1].entityID != 3 || items[1].authoritative || items[1].snapshotID != "import-observation" {
		t.Fatalf("unexpected public recipe selection: %#v", items)
	}

	recipe := map[string]any{"_recipeEntityId": int64(2), "_authoritative": true, "revisionId": "revision70"}
	server := &Server{db: db}
	if err = server.hydratePublicRecipeRenderLayouts(ctx, []map[string]any{recipe}); err != nil {
		t.Fatal(err)
	}
	layout, _ := recipe["layout"].(map[string]any)
	slots, _ := layout["slots"].([]any)
	if layout["layout_kind"] != "shaped" || layout["template_id"] != "crafting-grid" || len(slots) != 1 {
		t.Fatalf("unexpected canonical layout: %#v", layout)
	}
	slot, _ := slots[0].(map[string]any)
	alternatives, _ := slot["alternatives"].([]any)
	if slot["role"] != "input" || slot["ingredient_present"] != true || len(alternatives) != 1 {
		t.Fatalf("unexpected canonical slot: %#v", slot)
	}
	candidate, _ := alternatives[0].(map[string]any)
	if candidate["id"] != "minecraft:stone" || candidate["amount"] != float64(2) || candidate["probability"] != float64(0.5) {
		t.Fatalf("unexpected canonical candidate: %#v", candidate)
	}

	if _, err = db.Exec(ctx, `insert into catalog_entities(id,public_id,status,default_locale)
		values(6,'type00006','active','en-US'),(7,'type00007','active','en-US');
		insert into recipe_types(entity_id,canonical_id) values(6,'example:machine'),(7,'example:fallback');
		insert into recipe_type_catalysts(recipe_type_id,resource_id,ordinal) values(1,4,0),(6,4,0);
		insert into recipe_type_import_snapshots(id,recipe_type_id,revision_id,catalysts)
		values('bad-catalysts',7,'import-revision','{"not":"an array"}')`); err != nil {
		t.Fatal(err)
	}
	grouped, err := server.catalogRecipeTypesCatalysts(ctx, []int64{1, 6}, "en-US", "zh-CN", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(grouped[1]) != 1 || len(grouped[6]) != 1 || grouped[1][0]["id"] != "minecraft:stone" {
		t.Fatalf("unexpected batched canonical catalysts: %#v", grouped)
	}
	if _, err = server.catalogRecipeTypesCatalysts(ctx, []int64{7}, "en-US", "zh-CN", true); err == nil || !strings.Contains(err.Error(), "import-revision") {
		t.Fatalf("batched import fallback swallowed invalid catalysts: %v", err)
	}
}

func openGlobalCatalogTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return openNotificationTemplateTestDB(t)
}

func createGlobalRecipeAuthorityTestTables(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	if _, err := db.Exec(ctx, `create temp table mods(id bigint primary key,slug text not null,review_status text not null default 'approved');
		create temp table catalog_entities(id bigint primary key,public_id text not null,status text not null,default_locale text not null);
		create temp table content_revisions(id bigint primary key,public_id text not null);
		create temp table catalog_import_revisions(
			id text primary key,mod_id bigint not null,package_id text not null,is_active boolean not null,status text not null,
			activated_at timestamptz,created_at timestamptz not null);
		create temp table recipe_types(entity_id bigint primary key,canonical_id text not null);
		create temp table recipes(
			entity_id bigint primary key,recipe_type_id bigint not null,canonical_source_id text,semantic_fingerprint text not null,
			owner_mod_id bigint,identity_source text not null);
		create temp table recipe_import_snapshots(
			id text primary key,recipe_id bigint not null,revision_id text not null,source_recipe_id text not null,source_id_kind text not null);
		create temp table recipe_content_overrides(recipe_id bigint primary key,note text,layout_override jsonb);
		create temp table recipe_layout_templates(
			entity_id bigint primary key,recipe_type_id bigint not null,template_key text not null,canvas_width integer not null,
			canvas_height integer not null,image_scale integer not null);
		create temp table recipe_template_slots(
			id bigint primary key,template_id bigint not null,slot_key text not null,role text not null,ordinal integer not null,
			x numeric not null,y numeric not null,width numeric not null,height numeric not null,definition jsonb not null);
		create temp table recipe_definitions(recipe_id bigint primary key,template_id bigint not null,definition jsonb not null,published_revision_id bigint);
		create temp table recipe_bindings(id bigint primary key,recipe_id bigint not null,template_slot_id bigint not null,ordinal integer not null,definition jsonb not null);
		create temp table recipe_binding_candidates(
			binding_id bigint not null,candidate_index integer not null,resource_id bigint not null,amount numeric not null,
			probability numeric,byproduct boolean not null,definition jsonb not null);
		create temp table game_resources(entity_id bigint primary key,kind_code text not null,canonical_id text not null,namespace text not null,owner_mod_id bigint,created_from_revision_id text);
		create temp table mod_content_versions(id bigint primary key,mod_id bigint,status text);
        create temp table mod_resource_version_details(resource_id bigint,version_id bigint,status text);
        create temp table content_localizations(catalog_entity_id bigint,locale text not null,name text not null);
		create temp table resource_import_snapshots(id text primary key,resource_id bigint not null,revision_id text not null,icon_path text not null);
		create temp table oss_files(id bigint primary key,public_id text not null);
		create temp table catalog_resource_definitions(resource_id bigint primary key,icon_file_id bigint);
		create temp table recipe_type_catalysts(recipe_type_id bigint not null,resource_id bigint not null,ordinal integer not null);
		create temp table recipe_type_import_snapshots(id text primary key,recipe_type_id bigint not null,revision_id text not null,catalysts jsonb not null)`); err != nil {
		t.Fatal(err)
	}
}
