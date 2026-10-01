package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

// This fixture is synthetic and lives in the marker-owned disposable database
// created by isolatedAITestDatabase. No supplier or object storage is contacted.
func TestModExportEntryAndMetricVisibilityAuditIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	cache := querycache.New(config.RedisConfig{})
	t.Cleanup(func() { _ = cache.Close() })
	s := &Server{db: pool, cfg: cfg, cache: cache}
	var modID, versionID, entityID, actorID int64
	var publicID string
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('synthetic_importer','synthetic_importer@invalid.example','fixture') returning id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
 values(new_public_id(),'synthetic-entry-audit','Synthetic entry audit','approved',$1) returning id`, actorID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status)
 values($1,'Synthetic version',array['1.20.1'],array['forge'],'active') returning id`, modID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		sql  string
		args []any
	}{
		{`insert into catalog_import_packages(id,sha256,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest)
 values('entry-audit-package',$1,'synthetic.zip','mcmods-export/v1','fixture','1.20.1','forge','{}')`, []any{strings.Repeat("a", 64)}},
		{`insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version,status)
 values('entry-audit-job',$1,'entry-audit-package',$2,'fixture','ready')`, []any{modID, versionID}},
		{`insert into catalog_import_revisions(id,mod_id,package_id,job_id,target_version_id,revision_no,status,minecraft_version,loader,exporter_version,source_namespace,is_active,submitted_by)
 values('entry-audit-revision',$1,'entry-audit-package','entry-audit-job',$2,1,'ready','1.20.1','forge','fixture','synthetic',true,$3)`, []any{modID, versionID, actorID}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type) values('synthetic:entry-audit','resource') returning id,public_id`).Scan(&entityID, &publicID); err != nil {
		t.Fatal(err)
	}
	statements = []struct {
		sql  string
		args []any
	}{
		{`insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved) values($1,'minecraft.item','synthetic:audit_item','synthetic','audit_item',$2,true)`, []any{entityID, modID}},
		{`insert into mod_resource_bindings(resource_id,mod_id) values($1,$2)`, []any{entityID, modID}},
		{`insert into resource_import_snapshots(id,resource_id,revision_id,registry,names,data)
 values('entry-audit-snapshot',$1,'entry-audit-revision','items','{"en-US":"Synthetic English item","zh-CN":"合成测试物品"}','{}')`, []any{entityID}},
		{`insert into knowledge_pages(entity_id,locale,content_markdown) values($1,'en-US','Synthetic English page'),($1,'zh-CN','合成中文页面')`, []any{entityID}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	requestEntry := func(query, language string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/mod-exports/revisions/entry-audit-revision/entry?registry=items&entityId="+publicID+query, nil).WithContext(ctx)
		request.SetPathValue("revisionId", "entry-audit-revision")
		request.Header.Set("Accept-Language", language)
		response := httptest.NewRecorder()
		s.modExportEntryDetail(response, request)
		return response
	}
	decodeEntry := func(t *testing.T, response *httptest.ResponseRecorder) modExportEntryDetailResponse {
		t.Helper()
		if response.Code != http.StatusOK {
			t.Fatalf("entry status=%d body=%s", response.Code, response.Body.String())
		}
		var value struct {
			Data modExportEntryDetailResponse `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		return value.Data
	}
	t.Run("available versions use the public identity", func(t *testing.T) {
		value := decodeEntry(t, requestEntry("&locale=en-US", "en-US"))
		if len(value.Versions) != 1 || value.Versions[0]["modSiteId"] != "synthetic-entry-audit" {
			t.Fatalf("missing approved version: %#v", value.Versions)
		}
	})
	t.Run("absent locale uses the request preference", func(t *testing.T) {
		value := decodeEntry(t, requestEntry("", "en-US"))
		if value.Name != "Synthetic English item" || value.ContentMarkdown != "Synthetic English page" || value.ContentLocale != "en-US" {
			t.Fatalf("wrong locale resolution: %#v", value)
		}
	})
	t.Run("explicit locale overrides request preference", func(t *testing.T) {
		value := decodeEntry(t, requestEntry("&locale=zh-CN", "en-US"))
		if value.Name != "合成测试物品" || value.ContentMarkdown != "合成中文页面" {
			t.Fatalf("explicit locale ignored: %#v", value)
		}
	})
	t.Run("knowledge page database failure is not a successful empty page", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `alter table knowledge_pages rename to synthetic_saved_knowledge_pages`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := pool.Exec(ctx, `alter table synthetic_saved_knowledge_pages rename to knowledge_pages`); err != nil {
				t.Error(err)
			}
		})
		response := requestEntry("&locale=en-US", "en-US")
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("database failure hidden: status=%d", response.Code)
		}
	})
	t.Run("approved importer appears in public metric attribution", func(t *testing.T) {
		actors, err := s.loadRecentMetricEditors(ctx, metricTarget{InternalID: entityID, Type: "resource"})
		if err != nil {
			t.Fatal(err)
		}
		if len(actors) != 1 || actors[0].Name != "synthetic_importer" {
			t.Fatalf("missing approved attribution: %#v", actors)
		}
	})

	// A second active import observes the same resource, but its parent is pending.
	var otherModID, otherVersionID, recipeTypeID, recipeID int64
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
	 values(new_public_id(),'synthetic-other-entry','Synthetic pending recipe','pending') returning id`).Scan(&otherModID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status)
	 values($1,'Synthetic alternate version',array['1.20.1'],array['forge'],'active') returning id`, otherModID).Scan(&otherVersionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type) values('synthetic:audit-recipe-type','recipe_type') returning id`).Scan(&recipeTypeID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type) values('synthetic:audit-recipe','recipe') returning id`).Scan(&recipeID); err != nil {
		t.Fatal(err)
	}
	statements = []struct {
		sql  string
		args []any
	}{
		{`insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version,status)
	 values('entry-other-job',$1,'entry-audit-package',$2,'fixture','ready')`, []any{otherModID, otherVersionID}},
		{`insert into catalog_import_revisions(id,mod_id,package_id,job_id,target_version_id,revision_no,status,minecraft_version,loader,exporter_version,source_namespace,is_active)
	 values('entry-other-revision',$1,'entry-audit-package','entry-other-job',$2,1,'ready','1.20.1','forge','fixture','synthetic',true)`, []any{otherModID, otherVersionID}},
		{`insert into recipe_types(entity_id,canonical_id) values($1,'synthetic:audit_type')`, []any{recipeTypeID}},
		{`insert into recipes(entity_id,recipe_type_id,canonical_source_id,semantic_fingerprint,identity_source)
	 values($1,$2,'synthetic:audit_recipe','synthetic-fingerprint','minecraft_recipe')`, []any{recipeID, recipeTypeID}},
		{`insert into recipe_type_import_snapshots(id,recipe_type_id,revision_id) values('entry-other-type',$1,'entry-other-revision')`, []any{recipeTypeID}},
		{`insert into recipe_template_import_snapshots(id,recipe_type_snapshot_id,revision_id,recipe_type_id,source_template_id,schema_version,template_collection_path,background_path)
	 values('entry-other-template','entry-other-type','entry-other-revision',$1,'synthetic-template','mcmods-jei-layout-template/v2','synthetic.json','')`, []any{recipeTypeID}},
		{`insert into recipe_template_import_slots(id,template_id,source_slot_id,role,ordinal)
	 values('entry-other-slot','entry-other-template','output','output',0)`, nil},
		{`insert into recipe_import_snapshots(id,recipe_id,revision_id,source_recipe_id,source_id_kind,source_recipe_key,recipe_collection_path,template_id)
	 values('entry-other-recipe',$1,'entry-other-revision','synthetic:audit_recipe','minecraft_recipe','synthetic-key','synthetic.json','entry-other-template')`, []any{recipeID}},
		{`insert into recipe_import_bindings(id,recipe_snapshot_id,template_slot_id,source_slot_id,ordinal,ingredient_present)
	 values('entry-other-binding','entry-other-recipe','entry-other-slot','output',0,true)`, nil},
		{`insert into recipe_import_binding_candidates(binding_id,alternative_index,resource_id,raw_resource_id,ingredient_kind,ingredient_type)
	 values('entry-other-binding',0,$1,'synthetic:audit_item','item','minecraft:item_stack')`, []any{entityID}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("pending alternate recipe stays private", func(t *testing.T) {
		produces, uses, err := s.modExportRecipesForObject(ctx, "entry-audit-revision", entityID, "en-US")
		if err != nil {
			t.Fatal(err)
		}
		if len(produces) != 0 || len(uses) != 0 {
			t.Fatalf("pending alternate recipe leaked: produces=%v uses=%v", produces, uses)
		}
	})
	if _, err := pool.Exec(ctx, `update mods set review_status='approved' where id=$1`, otherModID); err != nil {
		t.Fatal(err)
	}
	t.Run("approved alternate recipe remains available", func(t *testing.T) {
		produces, _, err := s.modExportRecipesForObject(ctx, "entry-audit-revision", entityID, "en-US")
		if err != nil {
			t.Fatal(err)
		}
		if len(produces) != 1 {
			t.Fatalf("approved alternate recipe lost: %v", produces)
		}
	})
	if _, err := pool.Exec(ctx, `update mod_content_versions set status='archived' where id=$1`, otherVersionID); err != nil {
		t.Fatal(err)
	}
	t.Run("archived alternate version stays private", func(t *testing.T) {
		produces, uses, err := s.modExportRecipesForObject(ctx, "entry-audit-revision", entityID, "en-US")
		if err != nil {
			t.Fatal(err)
		}
		if len(produces) != 0 || len(uses) != 0 {
			t.Fatalf("archived alternate version leaked: produces=%v uses=%v", produces, uses)
		}
	})

	if _, err := pool.Exec(ctx, `update mods set review_status='pending' where id=$1`, modID); err != nil {
		t.Fatal(err)
	}
	t.Run("pending importer is absent from public metric attribution", func(t *testing.T) {
		actors, err := s.loadRecentMetricEditors(ctx, metricTarget{InternalID: entityID, Type: "resource"})
		if err != nil {
			t.Fatal(err)
		}
		if len(actors) != 0 {
			t.Fatalf("pending import attribution leaked: %#v", actors)
		}
	})
	t.Run("pending parent resource statistics are not public", func(t *testing.T) {
		_, err := s.resolveMetricTarget(ctx, publicID)
		if err != pgx.ErrNoRows {
			t.Fatalf("pending resource statistics visible: error=%v", err)
		}
	})
	t.Run("hidden resource view does not mutate counters", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/content/"+publicID+"/view", nil).WithContext(ctx)
		request.SetPathValue("publicId", publicID)
		response := httptest.NewRecorder()
		s.recordMetricView(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("hidden resource view recorded: status=%d", response.Code)
		}
		var count int
		if err := pool.QueryRow(ctx, `select count(*) from content_view_daily`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("hidden view changed %d counters", count)
		}
	})
	t.Run("ownerless manual resource remains public", func(t *testing.T) {
		var manualID int64
		var manualPublicID string
		if err := pool.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type) values('synthetic:manual-metric','resource') returning id,public_id`).Scan(&manualID, &manualPublicID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path) values($1,'minecraft.item','synthetic:manual_metric','synthetic','manual_metric')`, manualID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.resolveMetricTarget(ctx, manualPublicID); err != nil {
			t.Fatalf("manual resource hidden: %v", err)
		}
	})
	t.Run("resource with an approved binding remains public", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `update mod_resource_bindings set mod_id=$2 where resource_id=$1`, entityID, otherModID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.resolveMetricTarget(ctx, publicID); err != nil {
			t.Fatalf("approved bound identity hidden: %v", err)
		}
	})

}
