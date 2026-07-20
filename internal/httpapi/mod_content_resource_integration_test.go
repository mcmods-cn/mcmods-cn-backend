package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

// This test exercises the real PostgreSQL JSON aggregation used by the public
// manual-resource endpoints. It is opt-in because it creates a short-lived,
// committed fixture so the handlers can query it through their pool.
func TestModContentResourceVersionDetailsIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run the mod content resource integration test")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	suffix := time.Now().UnixNano() % 1_000_000
	projectCode := fmt.Sprintf("mcr%06d", suffix)
	resourcePublicID := fmt.Sprintf("rcr%06d", suffix)
	slug := fmt.Sprintf("content-resource-smoke-%06d", suffix)
	resourceID := fmt.Sprintf("resource:content-resource-smoke:%06d", suffix)
	canonicalID := fmt.Sprintf("smokemod:gear_%06d", suffix)
	var modID, detailedVersionID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values($1,$2,'Content resource smoke','approved') returning id`, projectCode, slug).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `delete from mods where id=$1`, modID)
		_, _ = pool.Exec(ctx, `delete from catalog_entities where id=$1`, resourceID)
	}()
	if _, err = pool.Exec(ctx, `insert into catalog_entities(id,public_id,entity_type,status) values($1,$2,'resource','active')`, resourceID, resourcePublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved)
		values($1,'minecraft.item',$2,'smokemod',$3,$4,true)`, resourceID, canonicalID, fmt.Sprintf("gear_%06d", suffix), modID); err != nil {
		t.Fatal(err)
	}
	var detailedVersionPublicID, missingVersionPublicID string
	if err = pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,mod_version,status)
		values($1,'1.20.1 / Forge',array['1.20.1'],array['forge'],'1.0.0','active') returning id,public_id`, modID).Scan(&detailedVersionID, &detailedVersionPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,mod_version,status)
		values($1,'1.21.1 / NeoForge',array['1.21.1'],array['neoforge'],'2.0.0','active') returning public_id`, modID).Scan(&missingVersionPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_resource_bindings(resource_id,mod_id) values($1,$2)`, resourceID, modID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_resource_version_details(resource_id,version_id,default_locale,definition,status)
		values($1,$2,'zh-CN','{"hardness":3,"tool":"pickaxe"}'::jsonb,'active')`, resourceID, detailedVersionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_resource_version_detail_localizations(resource_id,version_id,locale,name,summary,content_markdown)
		values($1,$2,'zh-CN','版本名称','版本简介','版本正文')`, resourceID, detailedVersionID); err != nil {
		t.Fatal(err)
	}
	var templateID, sectionID int64
	var sectionPublicID string
	if err = pool.QueryRow(ctx, `select id from mod_content_templates where builtin order by id limit 1`).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_sections(mod_id,version_id,template_id,default_locale,display_mode,status)
		values($1,$2,$3,'zh-CN','compact','active') returning id,public_id`, modID, detailedVersionID, templateID).Scan(&sectionID, &sectionPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_content_section_localizations(section_id,locale,name,description)
		values($1,'zh-CN','测试分类','测试分类简介')`, sectionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_content_section_resources(section_id,version_id,resource_id,ordinal)
		values($1,$2,$3,0)`, sectionID, detailedVersionID, resourceID); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/mods/"+slug+"/content-resources", nil)
	listRequest.SetPathValue("siteId", slug)
	listResponse := httptest.NewRecorder()
	server.modContentResources(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list returned %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var listPayload struct {
		Data struct {
			Items []struct {
				PublicID string `json:"publicId"`
				Details  []struct {
					VersionPublicID string         `json:"versionPublicId"`
					Definition      map[string]any `json:"definition"`
				} `json:"details"`
			} `json:"items"`
		} `json:"data"`
	}
	if err = json.Unmarshal(listResponse.Body.Bytes(), &listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Data.Items) != 1 || len(listPayload.Data.Items[0].Details) != 1 || listPayload.Data.Items[0].Details[0].VersionPublicID != detailedVersionPublicID || listPayload.Data.Items[0].Details[0].Definition["hardness"] != float64(3) {
		t.Fatalf("unexpected resource list payload: %s", listResponse.Body.String())
	}

	sectionRequest := httptest.NewRequest(http.MethodGet, "/api/v1/mods/"+slug+"/content-sections", nil)
	sectionRequest.SetPathValue("siteId", slug)
	sectionResponse := httptest.NewRecorder()
	server.modContentSections(sectionResponse, sectionRequest)
	if sectionResponse.Code != http.StatusOK {
		t.Fatalf("sections returned %d: %s", sectionResponse.Code, sectionResponse.Body.String())
	}
	var sectionPayload struct {
		Data struct {
			Items []struct {
				ResourceCount int             `json:"resourceCount"`
				Resources     json.RawMessage `json:"resources"`
			} `json:"items"`
		} `json:"data"`
	}
	if err = json.Unmarshal(sectionResponse.Body.Bytes(), &sectionPayload); err != nil {
		t.Fatal(err)
	}
	if len(sectionPayload.Data.Items) != 1 || sectionPayload.Data.Items[0].ResourceCount != 1 || len(sectionPayload.Data.Items[0].Resources) != 0 {
		t.Fatalf("unexpected content section payload: %s", sectionResponse.Body.String())
	}

	sectionPageRequest := httptest.NewRequest(http.MethodGet, "/api/v1/mods/"+slug+"/content-sections/section/resources?locale=zh-CN&limit=20", nil)
	sectionPageRequest.SetPathValue("siteId", slug)
	sectionPageRequest.SetPathValue("sectionId", sectionPublicID)
	sectionPageResponse := httptest.NewRecorder()
	server.modContentSectionResources(sectionPageResponse, sectionPageRequest)
	if sectionPageResponse.Code != http.StatusOK {
		t.Fatalf("section resource page returned %d: %s", sectionPageResponse.Code, sectionPageResponse.Body.String())
	}
	var sectionPagePayload struct {
		Data struct {
			Total int `json:"total"`
			Items []struct {
				CanonicalID string            `json:"canonicalId"`
				KindCode    string            `json:"kindCode"`
				Names       map[string]string `json:"names"`
				Definition  map[string]any    `json:"definition"`
			} `json:"items"`
		} `json:"data"`
	}
	if err = json.Unmarshal(sectionPageResponse.Body.Bytes(), &sectionPagePayload); err != nil {
		t.Fatal(err)
	}
	if sectionPagePayload.Data.Total != 1 || len(sectionPagePayload.Data.Items) != 1 || sectionPagePayload.Data.Items[0].CanonicalID != canonicalID ||
		sectionPagePayload.Data.Items[0].KindCode != "minecraft.item" || len(sectionPagePayload.Data.Items[0].Definition) != 0 {
		t.Fatalf("unexpected paginated content section payload: %s", sectionPageResponse.Body.String())
	}

	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/mods/"+slug+"/content-resources/"+resourcePublicID, nil)
	detailRequest.SetPathValue("siteId", slug)
	detailRequest.SetPathValue("resourceId", resourcePublicID)
	detailResponse := httptest.NewRecorder()
	server.modContentResource(detailResponse, detailRequest)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("detail returned %d: %s", detailResponse.Code, detailResponse.Body.String())
	}
	var detailPayload struct {
		Data struct {
			Details []struct {
				VersionPublicID string `json:"versionPublicId"`
			} `json:"details"`
			Versions []struct {
				PublicID  string `json:"publicId"`
				HasDetail bool   `json:"hasDetail"`
			} `json:"versions"`
		} `json:"data"`
	}
	if err = json.Unmarshal(detailResponse.Body.Bytes(), &detailPayload); err != nil {
		t.Fatal(err)
	}
	if len(detailPayload.Data.Details) != 1 || detailPayload.Data.Details[0].VersionPublicID != detailedVersionPublicID || len(detailPayload.Data.Versions) != 2 {
		t.Fatalf("unexpected resource detail payload: %s", detailResponse.Body.String())
	}
	states := map[string]bool{}
	for _, version := range detailPayload.Data.Versions {
		states[version.PublicID] = version.HasDetail
	}
	if !states[detailedVersionPublicID] || states[missingVersionPublicID] {
		t.Fatalf("version content was inherited across versions: %s", detailResponse.Body.String())
	}
}
