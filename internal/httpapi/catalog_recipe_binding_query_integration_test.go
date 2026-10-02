package httpapi

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestCatalogRecipeBindingsUseOneQueryAtMaximumSlotScaleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify catalog recipe binding query count")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	counter := &integrationQueryCounter{}
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	poolConfig.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create temp table recipe_template_slots(id bigint primary key,slot_key text not null);
		create temp table recipe_bindings(
			id bigint primary key,recipe_id bigint not null,template_slot_id bigint not null,ordinal integer not null,definition jsonb not null);
		create temp table recipe_binding_candidates(
			id bigint primary key,binding_id bigint not null,candidate_index integer not null,resource_id bigint not null,
			amount numeric not null,probability numeric,byproduct boolean not null,definition jsonb not null);
		create temp table game_resources(entity_id bigint primary key,kind_code text not null,canonical_id text not null,resolved boolean not null,owner_mod_id bigint,created_from_revision_id text);
		create temp table catalog_entities(id bigint primary key,public_id text not null);
		create temp table catalog_resource_definitions(resource_id bigint primary key,icon_file_id bigint);
		create temp table oss_files(id bigint primary key,public_id text not null,status text not null);
		create temp table mods(id bigint primary key,review_status text not null);
        create temp table catalog_import_revisions(id text primary key,mod_id bigint,status text,is_active boolean);
        create temp table mod_content_versions(id bigint primary key,mod_id bigint,status text);
        create temp table mod_resource_version_details(resource_id bigint,version_id bigint,status text);
        insert into mods values(1,'approved');
        insert into catalog_import_revisions values('revision-1',1,'ready',true);
        create temp table resource_import_snapshots(
			id bigint primary key,resource_id bigint not null,revision_id text not null,icon_path text not null,created_at timestamptz not null);
		insert into catalog_entities values(1,'resource1'),(2,'resource2');
		insert into game_resources(entity_id,kind_code,canonical_id,resolved) values(1,'minecraft.item','minecraft:stone',true),(2,'minecraft.item','unknown:raw',false);
		insert into oss_files values(1,'iconfile1','active');
		insert into catalog_resource_definitions values(1,1);
		insert into resource_import_snapshots values(1,1,'revision-1','icons/stone.png',now());
		insert into recipe_template_slots values(999,'empty-slot');
		insert into recipe_bindings values(999,99,999,0,'{"empty":true}')`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	counter.queries.Store(0)
	empty, err := server.catalogRecipeBindingRows(ctx, 99)
	if err != nil {
		t.Fatal(err)
	}
	emptyBinding, ok := empty["empty-slot"].(map[string]any)
	emptyCandidates, candidatesOK := emptyBinding["candidates"].([]map[string]any)
	if !ok || !candidatesOK || len(emptyCandidates) != 0 || counter.queries.Load() != 1 {
		t.Fatalf("empty binding was not preserved by one query: binding=%#v queries=%d", emptyBinding, counter.queries.Load())
	}
	for _, fixture := range []struct {
		recipeID int64
		slots    int
	}{{1, 1}, {2, 64}, {3, 512}} {
		offset := fixture.recipeID * 1000
		if _, err = pool.Exec(ctx, `insert into recipe_template_slots(id,slot_key)
			select $1+value,'slot-' || lpad(value::text,4,'0') from generate_series(1,$2) value`, offset, fixture.slots); err != nil {
			t.Fatalf("load %d slots: %v", fixture.slots, err)
		}
		if _, err = pool.Exec(ctx, `insert into recipe_bindings(id,recipe_id,template_slot_id,ordinal,definition)
			select $1+value,$3,$1+value,value-1,jsonb_build_object('slot',value) from generate_series(1,$2) value`,
			offset, fixture.slots, fixture.recipeID); err != nil {
			t.Fatalf("load %d bindings: %v", fixture.slots, err)
		}
		if _, err = pool.Exec(ctx, `insert into recipe_binding_candidates(id,binding_id,candidate_index,resource_id,amount,probability,byproduct,definition)
			select $1+value,$1+value,0,1,1,null,false,jsonb_build_object('candidate',0) from generate_series(1,$2) value`,
			offset, fixture.slots); err != nil {
			t.Fatalf("load %d candidates: %v", fixture.slots, err)
		}
		if _, err = pool.Exec(ctx, `insert into recipe_binding_candidates(id,binding_id,candidate_index,resource_id,amount,probability,byproduct,definition)
			values($1::bigint+$2::bigint+10000,$1::bigint+$2::bigint,1,2,2,0.5,false,'{"candidate":1}')`, offset, fixture.slots); err != nil {
			t.Fatalf("load %d slots: %v", fixture.slots, err)
		}

		counter.queries.Store(0)
		queryContext, cancelQuery := context.WithTimeout(ctx, 2*time.Second)
		bindings, bindingErr := server.catalogRecipeBindingRows(queryContext, fixture.recipeID)
		cancelQuery()
		if bindingErr != nil {
			t.Fatalf("read %d slots: %v", fixture.slots, bindingErr)
		}
		if len(bindings) != fixture.slots {
			t.Fatalf("recipe %d bindings=%d want=%d", fixture.recipeID, len(bindings), fixture.slots)
		}
		if queries := counter.queries.Load(); queries != 1 {
			t.Fatalf("recipe with %d slots executed %d SQL statements, want 1", fixture.slots, queries)
		}
		lastKey := fmt.Sprintf("slot-%04d", fixture.slots)
		last, ok := bindings[lastKey].(map[string]any)
		if !ok {
			t.Fatalf("recipe %d missing %s binding", fixture.recipeID, lastKey)
		}
		candidates, ok := last["candidates"].([]map[string]any)
		if !ok || len(candidates) != 2 {
			t.Fatalf("recipe %d last candidates=%#v", fixture.recipeID, last["candidates"])
		}
		if candidates[0]["amount"] != float64(1) || candidates[0]["probability"] != nil ||
			candidates[1]["amount"] != float64(2) || candidates[1]["probability"] != float64(0.5) ||
			candidates[0]["iconUrl"] != "/api/v1/catalog/resources/resource1/icon" || candidates[1]["unresolved"] != true ||
			candidates[1]["rawResourceId"] != "unknown:raw" || candidates[1]["iconUrl"] != nil {
			t.Fatalf("recipe %d candidate order/shape changed: %#v", fixture.recipeID, candidates)
		}
	}
}
