package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestCatalogEditorPublicVisibilityAndReadFailuresAuditIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	cache := querycache.New(config.RedisConfig{})
	t.Cleanup(func() { _ = cache.Close() })
	s := &Server{db: pool, cfg: cfg, cache: cache}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	var modID, versionID int64
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values(new_public_id(),'catalog-editor-synthetic','Synthetic pending catalog','pending') returning id`).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status) values($1,'Synthetic source',array['1.20.1'],array['forge'],'active') returning id`, modID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	exec(`insert into catalog_import_packages(id,sha256,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest) values('editor-package',$1,'synthetic.zip','mcmods-export/v1','fixture','1.20.1','forge','{}')`, strings.Repeat("c", 64))
	exec(`insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version,status) values('editor-job',$1,'editor-package',$2,'fixture','ready')`, modID, versionID)
	exec(`insert into catalog_import_revisions(id,mod_id,package_id,job_id,target_version_id,revision_no,status,minecraft_version,loader,exporter_version,source_namespace,is_active) values('editor-revision',$1,'editor-package','editor-job',$2,1,'ready','1.20.1','forge','fixture','synthetic',true)`, modID, versionID)
	type entity struct {
		id       int64
		publicID string
	}
	entities := map[string]entity{}
	for _, kind := range []string{"recipe_type", "recipe_template", "recipe", "tag", "resource"} {
		value := entity{}
		if err := pool.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type) values($1,$2) returning id,public_id`, "synthetic-editor:"+kind, kind).Scan(&value.id, &value.publicID); err != nil {
			t.Fatal(err)
		}
		entities[kind] = value
	}
	typeID, templateID, recipeID, tagID, resourceID := entities["recipe_type"].id, entities["recipe_template"].id, entities["recipe"].id, entities["tag"].id, entities["resource"].id
	exec(`insert into recipe_types(entity_id,canonical_id) values($1,'synthetic:editor_type')`, typeID)
	exec(`insert into recipe_type_import_snapshots(id,recipe_type_id,revision_id,title_names) values('editor-type',$1,'editor-revision','{"en-US":"Synthetic imported type"}')`, typeID)
	exec(`insert into recipe_template_import_snapshots(id,recipe_type_snapshot_id,revision_id,recipe_type_id,canonical_template_id,source_template_id,schema_version,template_collection_path,background_path) values('editor-template','editor-type','editor-revision',$1,$2,'synthetic-template','mcmods-jei-layout-template/v2','synthetic.json','')`, typeID, templateID)
	exec(`insert into recipe_layout_templates(entity_id,recipe_type_id,template_key,import_snapshot_id,canvas_width,canvas_height) values($1,$2,'synthetic-template','editor-template',32,32)`, templateID, typeID)
	exec(`insert into recipes(entity_id,recipe_type_id,canonical_source_id,semantic_fingerprint,identity_source) values($1,$2,'synthetic:editor_recipe','synthetic-fingerprint','minecraft_recipe')`, recipeID, typeID)
	exec(`insert into recipe_import_snapshots(id,recipe_id,revision_id,source_recipe_id,source_id_kind,source_recipe_key,recipe_collection_path,template_id) values('editor-recipe',$1,'editor-revision','synthetic:editor_recipe','minecraft_recipe','synthetic-key','synthetic.json','editor-template')`, recipeID)
	exec(`insert into catalog_tags(entity_id,registry,canonical_id) values($1,'minecraft:item','synthetic:editor_tag')`, tagID)
	exec(`insert into tag_import_snapshots(id,tag_id,revision_id) values('editor-tag',$1,'editor-revision')`, tagID)
	exec(`insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,resolved) values($1,'minecraft.item','synthetic:editor_item','synthetic','editor_item',true)`, resourceID)
	exec(`insert into tag_import_members(tag_snapshot_id,resource_id,raw_member_id,ordinal) values('editor-tag',$1,'synthetic:editor_item',0)`, resourceID)
	run := func(server *Server, handler http.HandlerFunc, pathKey, publicID string) *httptest.ResponseRecorder {
		t.Helper()
		requestCtx, done := context.WithTimeout(ctx, 2*time.Second)
		defer done()
		request := httptest.NewRequest(http.MethodGet, "/synthetic?locale=en-US", nil).WithContext(requestCtx)
		if pathKey != "" {
			request.SetPathValue(pathKey, publicID)
		}
		response := httptest.NewRecorder()
		handler(response, request)
		return response
	}
	handlers := []struct {
		name, key, id string
		handler       func(*Server) http.HandlerFunc
	}{
		{"type", "publicId", entities["recipe_type"].publicID, func(s *Server) http.HandlerFunc { return s.catalogRecipeTypeDetail }},
		{"template", "publicId", entities["recipe_template"].publicID, func(s *Server) http.HandlerFunc { return s.catalogRecipeTemplateDetail }},
		{"recipe", "publicId", entities["recipe"].publicID, func(s *Server) http.HandlerFunc { return s.catalogRecipeDetail }},
		{"tag", "publicId", entities["tag"].publicID, func(s *Server) http.HandlerFunc { return s.catalogTagDetail }},
		{"template-list", "typePublicId", entities["recipe_type"].publicID, func(s *Server) http.HandlerFunc { return s.catalogRecipeTemplates }},
		{"recipe-list", "typePublicId", entities["recipe_type"].publicID, func(s *Server) http.HandlerFunc { return s.catalogRecipes }},
	}
	for _, value := range handlers {
		t.Run("pending/"+value.name, func(t *testing.T) {
			response := run(s, value.handler(s), value.key, value.id)
			if response.Code != http.StatusNotFound {
				t.Fatalf("pending catalog visible: %d %s", response.Code, response.Body.String())
			}
		})
	}
	t.Run("pending tag does not leak list counts", func(t *testing.T) {
		response := run(s, s.catalogTags, "", "")
		var body struct {
			Data struct{ Total int } `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if response.Code != 200 || body.Data.Total != 0 {
			t.Fatalf("pending tag count leaked: %s", response.Body.String())
		}
	})
	exec(`update mods set review_status='approved' where id=$1`, modID)
	for _, value := range handlers {
		t.Run("approved/"+value.name, func(t *testing.T) {
			response := run(s, value.handler(s), value.key, value.id)
			if response.Code != http.StatusOK {
				t.Fatalf("approved catalog lost: %d %s", response.Code, response.Body.String())
			}
		})
	}
	t.Run("review status excludes another entity type with the same ID", func(t *testing.T) {
		if modID != typeID {
			exec(`insert into mods(id,project_code,slug,primary_name,review_status) values($1,new_public_id(),'catalog-editor-collision','Synthetic collision','approved')`, typeID)
		}
		var revisionID int64
		if err := pool.QueryRow(ctx, `insert into content_revisions(entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash)
		 values('mod',$1,'mod_metadata','synthetic-catalog-collision',1,'{}',$2) returning id`, typeID, strings.Repeat("d", 64)).Scan(&revisionID); err != nil {
			t.Fatal(err)
		}
		exec(`insert into change_requests(entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status)
		 values('mod',$1,'mod_metadata','synthetic-catalog-collision',$2,'rejected')`, typeID, revisionID)
		response := run(s, s.catalogRecipeTypeDetail, "publicId", entities["recipe_type"].publicID)
		var body struct {
			Data struct{ ReviewStatus string } `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if response.Code != 200 || body.Data.ReviewStatus != "approved" {
			t.Fatalf("unrelated mod review status leaked into catalog: %d %s", response.Code, response.Body.String())
		}
	})
	t.Run("unapproved projection locale stays unpublished", func(t *testing.T) {
		exec(`insert into content_localizations(subject_type,subject_id,catalog_entity_id,locale,name,review_status) values('recipe_type',$1,$1,'en-US','Synthetic pending translation','pending'),('recipe_type',$1,$1,'zh-CN','Synthetic approved translation','approved')`, typeID)
		response := run(s, s.catalogRecipeTypeDetail, "publicId", entities["recipe_type"].publicID)
		if response.Code != 200 || strings.Contains(response.Body.String(), "Synthetic pending translation") {
			t.Fatalf("pending localization published: %d %s", response.Code, response.Body.String())
		}
	})
	t.Run("tag listing does not fall back to pending translations", func(t *testing.T) {
		exec(`insert into content_localizations(subject_type,subject_id,catalog_entity_id,locale,name,review_status)
		 values('tag',$1,$1,'en-US','Synthetic pending tag label','pending')`, tagID)
		response := run(s, s.catalogTags, "", "")
		if response.Code != 200 || strings.Contains(response.Body.String(), "Synthetic pending tag label") {
			t.Fatalf("pending tag label leaked: %d %s", response.Code, response.Body.String())
		}
	})
	t.Run("tag member count excludes pending resource owners", func(t *testing.T) {
		var pendingModID int64
		if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		 values(new_public_id(),'catalog-editor-pending-resource','Synthetic pending resource owner','pending') returning id`).Scan(&pendingModID); err != nil {
			t.Fatal(err)
		}
		exec(`update game_resources set owner_mod_id=$2 where entity_id=$1`, resourceID, pendingModID)
		defer exec(`update game_resources set owner_mod_id=null where entity_id=$1`, resourceID)
		response := run(s, s.catalogTags, "", "")
		var body struct {
			Data struct {
				Items []struct{ MemberCount int } `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if response.Code != 200 || len(body.Data.Items) != 1 || body.Data.Items[0].MemberCount != 0 {
			t.Fatalf("pending resource member count leaked: %d %s", response.Code, response.Body.String())
		}
	})
	t.Run("manual entity without import stays public", func(t *testing.T) {
		var manualID int64
		var manualPublicID string
		if err := pool.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type) values('synthetic-editor:manual','recipe_type') returning id,public_id`).Scan(&manualID, &manualPublicID); err != nil {
			t.Fatal(err)
		}
		exec(`insert into recipe_types(entity_id,canonical_id) values($1,'synthetic:manual_type')`, manualID)
		response := run(s, s.catalogRecipeTypeDetail, "publicId", manualPublicID)
		if response.Code != 200 {
			t.Fatalf("manual entity hidden: %d %s", response.Code, response.Body.String())
		}
	})
	// Force the canonical binding reader to issue a real candidate query under a
	// one-connection pool. The outer row cursor must be closed before that query.
	var slotID, bindingID int64
	if err := pool.QueryRow(ctx, `insert into recipe_template_slots(identity_key,template_id,slot_key,role,ordinal,x,y,width,height) values('synthetic-editor:slot',$1,'output','output',0,0,0,16,16) returning id`, templateID).Scan(&slotID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into recipe_bindings(identity_key,recipe_id,template_slot_id,ordinal) values('synthetic-editor:binding',$1,$2,0) returning id`, recipeID, slotID).Scan(&bindingID); err != nil {
		t.Fatal(err)
	}
	exec(`insert into recipe_binding_candidates(identity_key,binding_id,candidate_index,resource_id) values('synthetic-editor:candidate',$1,0,$2)`, bindingID, resourceID)
	t.Run("recipe candidates exclude pending resource owners", func(t *testing.T) {
		var pendingModID int64
		if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		 values(new_public_id(),'catalog-editor-pending-candidate','Synthetic pending candidate owner','pending') returning id`).Scan(&pendingModID); err != nil {
			t.Fatal(err)
		}
		exec(`update game_resources set owner_mod_id=$2 where entity_id=$1`, resourceID, pendingModID)
		defer exec(`update game_resources set owner_mod_id=null where entity_id=$1`, resourceID)
		candidates, err := s.catalogRecipeCandidateRows(ctx, bindingID)
		if err != nil || len(candidates) != 0 {
			t.Fatalf("pending resource candidate leaked: %v error=%v", candidates, err)
		}
	})
	smallConfig := pool.Config().Copy()
	smallConfig.MinConns = 0
	smallConfig.MaxConns = 1
	small, err := pgxpool.NewWithConfig(ctx, smallConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(small.Close)
	smallServer := &Server{db: small, cfg: cfg, cache: cache}
	t.Run("one connection reads nested binding candidates", func(t *testing.T) {
		requestCtx, done := context.WithTimeout(ctx, 2*time.Second)
		defer done()
		bindings, err := smallServer.catalogRecipeBindingRows(requestCtx, recipeID)
		if err != nil || len(bindings) != 1 {
			t.Fatalf("binding reader self blocked: bindings=%v error=%v", bindings, err)
		}
	})
	t.Run("one connection reads recipe listing and version order", func(t *testing.T) {
		requestCtx, done := context.WithTimeout(ctx, 2*time.Second)
		defer done()
		request := httptest.NewRequest(http.MethodGet, "/synthetic?locale=en-US", nil).WithContext(requestCtx)
		request.SetPathValue("typePublicId", entities["recipe_type"].publicID)
		response := httptest.NewRecorder()
		smallServer.catalogRecipes(response, request)
		if response.Code != 200 || requestCtx.Err() != nil {
			t.Fatalf("recipe list self blocked: %d %s", response.Code, response.Body.String())
		}
	})
	for _, table := range []string{"content_localizations", "recipe_template_slots", "recipe_layout_templates", "catalog_tags", "recipe_types", "recipes", "game_resources"} {
		t.Run("stream failure/"+table, func(t *testing.T) {
			name := "catalog_fault_" + randomHex(6)
			schema := pgx.Identifier{name}.Sanitize()
			exec("create schema " + schema)
			exec("create function " + schema + ".fail_stream() returns boolean language plpgsql volatile as $$ begin raise exception 'synthetic catalog read failure'; end $$")
			exec("create view " + schema + "." + pgx.Identifier{table}.Sanitize() + " as select * from public." + pgx.Identifier{table}.Sanitize() + " where " + schema + ".fail_stream()")
			shadowConfig := pool.Config().Copy()
			shadowConfig.ConnConfig.RuntimeParams["search_path"] = name + ",public"
			shadow, err := pgxpool.NewWithConfig(ctx, shadowConfig)
			if err != nil {
				t.Fatal(err)
			}
			defer shadow.Close()
			server := &Server{db: shadow, cfg: cfg, cache: cache}
			var response *httptest.ResponseRecorder
			switch table {
			case "content_localizations":
				response = run(server, server.catalogRecipeTypeDetail, "publicId", entities["recipe_type"].publicID)
			case "recipe_template_slots":
				response = run(server, server.catalogRecipeTemplateDetail, "publicId", entities["recipe_template"].publicID)
				var edit catalogRecipeEdit
				if err := json.Unmarshal([]byte(`{"templatePublicId":"`+entities["recipe_template"].publicID+`","defaultLocale":"en-US","localizations":[{"locale":"en-US","name":"Synthetic normalized recipe"}]}`), &edit); err != nil {
					t.Fatal(err)
				}
				if err := server.normalizeCatalogRecipeEdit(ctx, typeID, &edit); err == nil || !strings.Contains(err.Error(), "synthetic catalog read failure") {
					t.Fatalf("normalized role stream failure lost: %v", err)
				}
			case "catalog_tags":
				response = run(server, server.catalogTagDetail, "publicId", entities["tag"].publicID)
			case "recipe_types":
				response = run(server, server.catalogRecipeTypeDetail, "publicId", entities["recipe_type"].publicID)
			case "recipes":
				response = run(server, server.catalogRecipeDetail, "publicId", entities["recipe"].publicID)
			case "game_resources":
				response = run(server, server.catalogResourceDetail, "publicId", entities["resource"].publicID)
			default:
				response = run(server, server.catalogRecipeTemplates, "typePublicId", entities["recipe_type"].publicID)
			}
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("%s stream error hidden: %d %s", table, response.Code, response.Body.String())
			}
		})
	}
}
