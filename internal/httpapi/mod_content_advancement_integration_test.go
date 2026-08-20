package httpapi

import (
	"bytes"
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
	"mcmods-cn-backend/internal/security"
)

func TestCreateAndArrangeAdvancementIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run the advancement save integration test")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	suffix := time.Now().UnixNano() % 1_000_000
	projectCode := fmt.Sprintf("agt%06d", suffix)
	slug := fmt.Sprintf("advancement-save-%06d", suffix)
	canonicalID := fmt.Sprintf("testmod:advancement_%06d", suffix)
	username := fmt.Sprintf("advancement-save-%06d", suffix)
	email := username + "@example.invalid"
	var actorID, modID, versionID, sectionID, templateID int64
	var versionPublicID, sectionPublicID string
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test',true) returning id`, username, email).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values($1,$2,'Advancement save test','pending',$3) returning id`, projectCode, slug, actorID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,mod_version,status,created_by)
		values($1,'1.21.1 / NeoForge',array['1.21.1'],array['neoforge'],'1.0.0','active',$2)
		returning id,public_id`, modID, actorID).Scan(&versionID, &versionPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from mod_content_templates where builtin and code='advancement' and status='active'`).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_sections(mod_id,version_id,template_id,default_locale,display_mode,status,created_by)
		values($1,$2,$3,'zh-CN','large','active',$4) returning id,public_id`,
		modID, versionID, templateID, actorID).Scan(&sectionID, &sectionPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_content_section_localizations(section_id,locale,name,description)
		values($1,'zh-CN','成就','成就')`, sectionID); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	claims := security.Claims{
		Subject: actorID,
		PermissionRules: []security.PermissionRule{
			{Code: "project.edit." + projectCode, Allow: true},
			{Code: "content.no-review", Allow: true},
		},
	}
	createBody, err := json.Marshal(map[string]any{
		"kindCode":        "minecraft.advancement",
		"canonicalId":     canonicalID,
		"versionPublicId": versionPublicID,
		"sectionPublicId": sectionPublicID,
		"entryTypeCode":   "advancement",
		"defaultLocale":   "zh-CN",
		"definition":      map[string]any{"display": map[string]any{"x": 0, "y": 0}},
		"localizations":   []map[string]any{{"locale": "zh-CN", "name": "测试成就"}},
		"reason":          "advancement create regression",
	})
	if err != nil {
		t.Fatal(err)
	}
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/mods/"+slug+"/content-resources", bytes.NewReader(createBody))
	createRequest.SetPathValue("siteId", slug)
	createRequest = createRequest.WithContext(context.WithValue(createRequest.Context(), claimsContextKey, claims))
	createResponse := httptest.NewRecorder()
	server.modContentResources(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create advancement returned %d: %s", createResponse.Code, createResponse.Body.String())
	}
	var createPayload struct {
		Data modContentMutationResult `json:"data"`
	}
	if err = json.Unmarshal(createResponse.Body.Bytes(), &createPayload); err != nil || createPayload.Data.PublicID == "" {
		t.Fatalf("decode create advancement response: %v: %s", err, createResponse.Body.String())
	}

	var resourceID int64
	var resourcePublicID string
	if err = pool.QueryRow(ctx, `select entity.id,entity.public_id from catalog_entities entity
		join game_resources resource on resource.entity_id=entity.id
		where resource.owner_mod_id=$1 and resource.kind_code='minecraft.advancement' and resource.canonical_id=$2`,
		modID, canonicalID).Scan(&resourceID, &resourcePublicID); err != nil {
		t.Fatal(err)
	}
	if createPayload.Data.ReviewStatus == "pending" {
		if _, err = pool.Exec(ctx, `update catalog_entities set status='active' where id=$1`, resourceID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `update mod_resource_version_details set status='active' where resource_id=$1 and version_id=$2`, resourceID, versionID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into mod_content_section_resources(section_id,version_id,resource_id,ordinal,placement_source)
			values($1,$2,$3,0,'manual')`, sectionID, versionID, resourceID); err != nil {
			t.Fatal(err)
		}
	}
	layoutPayload := map[string]any{
		"versionPublicId":     versionPublicID,
		"rootSectionPublicId": sectionPublicID,
		"displayMode":         "compact",
		"categories":          []any{},
		"resources": []map[string]any{{
			"resourcePublicId": resourcePublicID,
			"sectionPublicId":  sectionPublicID,
			"ordinal":          0,
			"advancement": map[string]any{
				"parentResourcePublicId": "",
				"groupId":                "advancement:test",
				"x":                      2,
				"y":                      3,
			},
		}},
		"reason": "advancement layout regression",
	}
	if createPayload.Data.ReviewStatus == "approved" {
		var baseRevisionPublicID string
		if err = pool.QueryRow(ctx, `select revision.public_id from mod_content_sections section
			join content_revisions revision on revision.id=section.published_revision_id where section.id=$1`,
			sectionID).Scan(&baseRevisionPublicID); err != nil {
			t.Fatal(err)
		}
		layoutPayload["baseRevisionId"] = baseRevisionPublicID
	}
	layoutBody, err := json.Marshal(layoutPayload)
	if err != nil {
		t.Fatal(err)
	}
	layoutRequest := httptest.NewRequest(http.MethodPut, "/api/v1/mods/"+slug+"/content-sections/"+sectionPublicID+"/layout", bytes.NewReader(layoutBody))
	layoutRequest.SetPathValue("siteId", slug)
	layoutRequest.SetPathValue("sectionId", sectionPublicID)
	layoutRequest = layoutRequest.WithContext(context.WithValue(layoutRequest.Context(), claimsContextKey, claims))
	layoutResponse := httptest.NewRecorder()
	server.modContentSectionLayout(layoutResponse, layoutRequest)
	if layoutResponse.Code != http.StatusOK {
		t.Fatalf("arrange advancement returned %d: %s", layoutResponse.Code, layoutResponse.Body.String())
	}

	var detailRevisionID int64
	if err = pool.QueryRow(ctx, `select published_revision_id from mod_resource_version_details
		where resource_id=$1 and version_id=$2`, resourceID, versionID).Scan(&detailRevisionID); err != nil {
		t.Fatal(err)
	}
	var nextBaseRevisionPublicID string
	if err = pool.QueryRow(ctx, `select revision.public_id from mod_content_sections section
		join content_revisions revision on revision.id=section.published_revision_id where section.id=$1`,
		sectionID).Scan(&nextBaseRevisionPublicID); err != nil {
		t.Fatal(err)
	}
	layoutPayload["baseRevisionId"] = nextBaseRevisionPublicID
	layoutBody, err = json.Marshal(layoutPayload)
	if err != nil {
		t.Fatal(err)
	}
	unchangedLayoutRequest := httptest.NewRequest(http.MethodPut, "/api/v1/mods/"+slug+"/content-sections/"+sectionPublicID+"/layout", bytes.NewReader(layoutBody))
	unchangedLayoutRequest.SetPathValue("siteId", slug)
	unchangedLayoutRequest.SetPathValue("sectionId", sectionPublicID)
	unchangedLayoutRequest = unchangedLayoutRequest.WithContext(context.WithValue(unchangedLayoutRequest.Context(), claimsContextKey, claims))
	unchangedLayoutResponse := httptest.NewRecorder()
	server.modContentSectionLayout(unchangedLayoutResponse, unchangedLayoutRequest)
	if unchangedLayoutResponse.Code != http.StatusOK {
		t.Fatalf("save unchanged advancement layout returned %d: %s", unchangedLayoutResponse.Code, unchangedLayoutResponse.Body.String())
	}
	var unchangedDetailRevisionID int64
	if err = pool.QueryRow(ctx, `select published_revision_id from mod_resource_version_details
		where resource_id=$1 and version_id=$2`, resourceID, versionID).Scan(&unchangedDetailRevisionID); err != nil {
		t.Fatal(err)
	}
	if unchangedDetailRevisionID != detailRevisionID {
		t.Fatalf("unchanged advancement detail revision changed from %d to %d", detailRevisionID, unchangedDetailRevisionID)
	}
}
