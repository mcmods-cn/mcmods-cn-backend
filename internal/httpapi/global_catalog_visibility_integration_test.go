package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestGlobalCatalogHidesUnapprovedActiveImportsAndUsesOneConnectionIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	var modID, versionID, entityID int64
	var publicID string
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values('auditcat1','audit-catalog-visibility','synthetic pending import','pending') returning id`).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status)
		values($1,'synthetic fixture',array['1.20.1'],array['forge'],'active') returning id`, modID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`insert into catalog_import_packages(id,sha256,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest)
		 values('audit-catalog-package',$1,'synthetic.zip','mcmods-export/v1','fixture','1.20.1','forge','{}')`, []any{strings.Repeat("a", 64)}},
		{`insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version,status)
		 values('audit-catalog-job',$1,'audit-catalog-package',$2,'fixture','ready')`, []any{modID, versionID}},
		{`insert into catalog_import_revisions(id,mod_id,package_id,job_id,target_version_id,revision_no,status,minecraft_version,loader,exporter_version,source_namespace,is_active)
		 values('audit-catalog-revision',$1,'audit-catalog-package','audit-catalog-job',$2,1,'ready','1.20.1','forge','fixture','audit',true)`, []any{modID, versionID}},
	} {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type)
		values('audit:recipe_type','recipe_type') returning id,public_id`).Scan(&entityID, &publicID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into recipe_types(entity_id,canonical_id) values($1,'audit:synthetic_type')`, entityID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into recipe_type_import_snapshots(id,recipe_type_id,revision_id,title_names)
		values('audit-type-snapshot',$1,'audit-catalog-revision','{"en-US":"Synthetic pending recipe"}')`, entityID); err != nil {
		t.Fatal(err)
	}
	var resourceID int64
	var resourcePublicID string
	if err := pool.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type) values('audit:resource-version','resource') returning id,public_id`).Scan(&resourceID, &resourcePublicID); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`insert into resource_kinds(code,family) values('audit.item','audit')`, nil},
		{`insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path) values($1,'audit.item','audit:fixture','audit','fixture')`, []any{resourceID}},
		{`insert into mod_resource_bindings(resource_id,mod_id) values($1,$2)`, []any{resourceID, modID}},
	} {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	poolConfig := pool.Config().Copy()
	poolConfig.MinConns, poolConfig.MaxConns = 0, 1
	apiPool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(apiPool.Close)
	cache := querycache.New(config.RedisConfig{})
	t.Cleanup(func() { _ = cache.Close() })
	s := &Server{db: apiPool, cfg: cfg, cache: cache}
	assertVersions := func(expected int) {
		t.Helper()
		items := []map[string]any{{"publicId": resourcePublicID}}
		if err := s.decorateResourceVersionRows(ctx, items, "en-US", "zh-CN"); err != nil {
			t.Fatal(err)
		}
		if got := len(items[0]["versions"].([]map[string]any)); got != expected {
			t.Fatalf("parent approval version visibility=%d want=%d", got, expected)
		}
	}
	assertVersions(0)
	list := func() (int, int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/recipe-types?locale=en-US", nil)
		requestCtx, done := context.WithTimeout(ctx, 3*time.Second)
		defer done()
		response := httptest.NewRecorder()
		s.globalRecipeTypes(response, request.WithContext(requestCtx))
		var value struct {
			Data struct {
				Total int
				Items []map[string]any
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		return response.Code, value.Data.Total
	}
	if status, total := list(); status != http.StatusOK || total != 0 {
		t.Fatalf("pending active import leaked into global catalog: status=%d total=%d", status, total)
	}
	response := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/recipe-types/"+publicID, nil)
	r.SetPathValue("publicId", publicID)
	s.globalRecipeTypeCatalog(response, r.WithContext(ctx))
	if response.Code != http.StatusNotFound {
		t.Fatalf("pending recipe type was visible by ID: status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := pool.Exec(ctx, `update mods set review_status='approved' where id=$1`, modID); err != nil {
		t.Fatal(err)
	}
	assertVersions(1)
	if status, total := list(); status != http.StatusOK || total != 1 {
		t.Fatalf("approved import failed in a one-connection pool: status=%d total=%d", status, total)
	}
	response = httptest.NewRecorder()
	requestCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()
	s.globalRecipeTypeCatalog(response, r.WithContext(requestCtx))
	if response.Code != http.StatusOK {
		t.Fatalf("approved catalog detail failed: status=%d body=%s", response.Code, response.Body.String())
	}
}
