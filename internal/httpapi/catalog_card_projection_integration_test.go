package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestCatalogCardHandlersBoundWorstCaseNestedDataIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify bounded catalog cards against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	if _, err = pool.Exec(ctx, `insert into simple_projects(
		project_type,slug,default_locale,primary_name,abbreviation,minecraft_versions,loaders,categories,features,
		official_status,source_status,license,review_status,published_at)
		select 'plugin','perf018-plugin-'||lpad(value::text,3,'0'),'zh-CN','PERF018 Plugin '||value,'P18',
		array['1.21.1'],array['paper'],array['utility'],array['locale'],'active','open','MIT','approved',now()
		from generate_series(1,100) value`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into simple_project_localizations(project_id,locale,name,summary,body_markdown)
		select project.id,language.locale,'PERF018 '||language.locale||' '||project.id,'summary '||language.locale,
			repeat(md5(project.id::text||language.locale),32768)
		from simple_projects project cross join (values
			('zh-CN'),('zh-TW'),('en-US'),('ja-JP'),('ru-RU'),('fr-FR'),('de-DE'),('es-ES')) language(locale)
		where project.slug like 'perf018-plugin-%'`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into simple_project_parent_refs(project_id,target_type,raw_identifier,display_order)
		select project.id,'mod','missing-parent-'||value,value
		from simple_projects project cross join generate_series(1,5) value
		where project.slug='perf018-plugin-001'`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into creators(kind,name,normalized_name,review_status)
		select 'author','PERF018 Author '||value,'perf018-author-'||value,'approved' from generate_series(1,12) value`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,status,display_order)
		select 'plugin',project.id,creator.id,'approved',row_number() over(order by creator.id)
		from simple_projects project cross join creators creator
		where project.slug='perf018-plugin-001' and creator.normalized_name like 'perf018-author-%'`); err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, `insert into modpacks(
		slug,primary_name,secondary_name,summary,default_locale,environment,primary_category,official_status,
		source_status,license,body_markdown,review_status,published_at)
		select 'perf018-pack-'||lpad(value::text,3,'0'),'PERF018 Pack '||value,'性能包 '||value,'bounded card',
			'zh-CN','bothRequired','optimization','active','open','MIT',repeat(md5(value::text),32768),'approved',now()
		from generate_series(1,100) value`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into modpack_loader_compatibilities(modpack_id,loader,minecraft_version)
		select pack.id,'loader-'||loader,'1.'||version from modpacks pack
		cross join generate_series(1,20) loader cross join generate_series(1,40) version
		where pack.slug='perf018-pack-001'`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into modpack_tags(modpack_id,tag)
		select pack.id,'tag-'||lpad(value::text,2,'0') from modpacks pack cross join generate_series(1,50) value
		where pack.slug='perf018-pack-001'`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,status,display_order)
		select 'modpack',pack.id,creator.id,'approved',row_number() over(order by creator.id)
		from modpacks pack cross join creators creator
		where pack.slug='perf018-pack-001' and creator.normalized_name like 'perf018-author-%'`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into modpack_mods(modpack_id,provider,identifier,mod_name,display_order)
		select pack.id,'manual','perf018:mod:'||value,'Contained Mod '||value,value
		from modpacks pack cross join generate_series(1,2000) value where pack.slug='perf018-pack-001'`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	simpleRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/content-projects/plugin?q=PERF018&sort=updated&order=desc&limit=100&locale=en-US", nil).WithContext(ctx)
	simpleRequest.SetPathValue("projectType", "plugin")
	simpleResponse := httptest.NewRecorder()
	server.simpleProjects(simpleResponse, simpleRequest)
	if simpleResponse.Code != http.StatusOK {
		t.Fatalf("simple-project catalog returned %d: %s", simpleResponse.Code, simpleResponse.Body.String())
	}
	assertCatalogResponseBudget(t, simpleResponse.Body.Len())
	t.Logf("simple-project catalog cards: items=100 bytes=%d", simpleResponse.Body.Len())
	var simpleEnvelope struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err = json.Unmarshal(simpleResponse.Body.Bytes(), &simpleEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(simpleEnvelope.Data.Items) != 100 {
		t.Fatalf("simple-project card count = %d, want 100", len(simpleEnvelope.Data.Items))
	}
	for _, item := range simpleEnvelope.Data.Items {
		assertCatalogCardOmits(t, item, "bodyMarkdown", "links", "galleryImages")
		assertNestedCardCap(t, item, "localizations", 2)
		assertNestedCardCap(t, item, "authors", 8)
		assertNestedCardCap(t, item, "parentProjects", 3)
	}

	modpackRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/modpacks?q=PERF018&sort=updated&order=desc&limit=100", nil).WithContext(ctx)
	modpackResponse := httptest.NewRecorder()
	server.modpacks(modpackResponse, modpackRequest)
	if modpackResponse.Code != http.StatusOK {
		t.Fatalf("modpack catalog returned %d: %s", modpackResponse.Code, modpackResponse.Body.String())
	}
	assertCatalogResponseBudget(t, modpackResponse.Body.Len())
	t.Logf("modpack catalog cards: items=100 bytes=%d", modpackResponse.Body.Len())
	var modpackEnvelope struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err = json.Unmarshal(modpackResponse.Body.Bytes(), &modpackEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(modpackEnvelope.Data.Items) != 100 {
		t.Fatalf("modpack card count = %d, want 100", len(modpackEnvelope.Data.Items))
	}
	for _, item := range modpackEnvelope.Data.Items {
		assertCatalogCardOmits(t, item, "bodyMarkdown", "mods", "links", "galleryImages")
		assertNestedCardCap(t, item, "authors", 8)
		assertNestedCardCap(t, item, "tags", 32)
		assertNestedCardCap(t, item, "compatibilities", 16)
	}

	simpleDetailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/content-projects/plugin/perf018-plugin-001", nil).WithContext(ctx)
	simpleDetailRequest.SetPathValue("projectType", "plugin")
	simpleDetailRequest.SetPathValue("siteId", "perf018-plugin-001")
	simpleDetailResponse := httptest.NewRecorder()
	server.simpleProjectItem(simpleDetailResponse, simpleDetailRequest)
	if simpleDetailResponse.Code != http.StatusOK || !jsonPathArrayHasLength(simpleDetailResponse.Body.Bytes(), "localizations", 8) ||
		!jsonPathStringHasLength(simpleDetailResponse.Body.Bytes(), "localizations", "bodyMarkdown", 1<<20) {
		t.Fatalf("simple-project detail no longer preserves full localizations: status=%d bytes=%d", simpleDetailResponse.Code, simpleDetailResponse.Body.Len())
	}

	modpackDetailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/modpacks/perf018-pack-001", nil).WithContext(ctx)
	modpackDetailRequest.SetPathValue("siteId", "perf018-pack-001")
	modpackDetailResponse := httptest.NewRecorder()
	server.modpackItem(modpackDetailResponse, modpackDetailRequest)
	if modpackDetailResponse.Code != http.StatusOK || !jsonPathArrayHasLength(modpackDetailResponse.Body.Bytes(), "mods", 2000) ||
		!jsonPathStringHasLength(modpackDetailResponse.Body.Bytes(), "", "bodyMarkdown", 1<<20) {
		t.Fatalf("modpack detail no longer preserves full content: status=%d bytes=%d", modpackDetailResponse.Code, modpackDetailResponse.Body.Len())
	}
}

func assertCatalogResponseBudget(t *testing.T, size int) {
	t.Helper()
	if size > maxCatalogResponseBytes {
		t.Fatalf("catalog response used %d bytes, budget is %d", size, maxCatalogResponseBytes)
	}
}

func assertCatalogCardOmits(t *testing.T, item map[string]any, fields ...string) {
	t.Helper()
	for _, field := range fields {
		if _, exists := item[field]; exists {
			t.Errorf("catalog card unexpectedly contains detail-only field %q", field)
		}
	}
}

func assertNestedCardCap(t *testing.T, item map[string]any, field string, maximum int) {
	t.Helper()
	values, ok := item[field].([]any)
	if !ok {
		t.Fatalf("catalog card field %q is not an array", field)
	}
	if len(values) > maximum {
		t.Errorf("catalog card field %q has %d items, cap is %d", field, len(values), maximum)
	}
}

func jsonPathArrayHasLength(raw []byte, field string, expected int) bool {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return false
	}
	values, ok := envelope.Data[field].([]any)
	return ok && len(values) == expected
}

func jsonPathStringHasLength(raw []byte, arrayField, stringField string, expected int) bool {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return false
	}
	if arrayField == "" {
		value, ok := envelope.Data[stringField].(string)
		return ok && len(value) == expected
	}
	values, ok := envelope.Data[arrayField].([]any)
	if !ok || len(values) == 0 {
		return false
	}
	first, ok := values[0].(map[string]any)
	if !ok {
		return false
	}
	value, ok := first[stringField].(string)
	return ok && len(value) == expected
}
