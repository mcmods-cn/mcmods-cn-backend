package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func openModContentSafetyTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run mod content safety integration tests")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestModContentResourcePreviewScopeAndSectionIntegration(t *testing.T) {
	pool := openModContentSafetyTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	suffix := time.Now().UnixNano() % 1_000_000
	firstSlug := fmt.Sprintf("content-scope-a-%06d", suffix)
	secondSlug := fmt.Sprintf("content-scope-b-%06d", suffix)
	var firstModID, secondModID int64
	if err = tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values($1,$2,'Scope A','approved') returning id`, randomCatalogPublicID(), firstSlug).Scan(&firstModID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values($1,$2,'Scope B','approved') returning id`, randomCatalogPublicID(), secondSlug).Scan(&secondModID); err != nil {
		t.Fatal(err)
	}
	var versionID int64
	var versionPublicID string
	if err = tx.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status)
		values($1,'1.21.1 / NeoForge',array['1.21.1'],array['neoforge'],'active') returning id,public_id`,
		secondModID).Scan(&versionID, &versionPublicID); err != nil {
		t.Fatal(err)
	}
	resourcePublicID := randomCatalogPublicID()
	var resourceID int64
	if err = tx.QueryRow(ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status)
		values($1,$2,'resource','active') returning id`,
		fmt.Sprintf("resource:scope:%06d", suffix), resourcePublicID).Scan(&resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved)
		values($1,'minecraft.item',$2,'scope',$3,$4,true)`,
		resourceID, fmt.Sprintf("scope:item_%06d", suffix), fmt.Sprintf("item_%06d", suffix), secondModID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into mod_resource_bindings(resource_id,mod_id) values($1,$2)`, resourceID, secondModID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into mod_resource_version_details(resource_id,version_id,default_locale,definition,status)
		values($1,$2,'en-US','{}'::jsonb,'pending')`, resourceID, versionID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from mods where id=any($1::bigint[])`, []int64{firstModID, secondModID})
		_, _ = pool.Exec(context.Background(), `delete from catalog_entities where id=$1`, resourceID)
	})

	server := &Server{db: pool}
	reviewerClaims := security.Claims{Permissions: []string{"content.review"}}
	previewRequest := httptest.NewRequest(http.MethodGet, "/api/v1/mods/"+secondSlug+"/content-resources/"+resourcePublicID, nil)
	previewRequest.SetPathValue("siteId", secondSlug)
	previewRequest.SetPathValue("resourceId", resourcePublicID)
	previewRequest = previewRequest.WithContext(context.WithValue(previewRequest.Context(), claimsContextKey, reviewerClaims))
	previewResponse := httptest.NewRecorder()
	server.modContentResource(previewResponse, previewRequest)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("content reviewer could not preview pending detail: %d %s", previewResponse.Code, previewResponse.Body.String())
	}

	crossScopeRequest := httptest.NewRequest(http.MethodGet, "/api/v1/mods/"+firstSlug+"/content-resources/"+resourcePublicID, nil)
	crossScopeRequest.SetPathValue("siteId", firstSlug)
	crossScopeRequest.SetPathValue("resourceId", resourcePublicID)
	crossScopeRequest = crossScopeRequest.WithContext(context.WithValue(crossScopeRequest.Context(), claimsContextKey, reviewerClaims))
	crossScopeResponse := httptest.NewRecorder()
	server.modContentResource(crossScopeResponse, crossScopeRequest)
	if crossScopeResponse.Code != http.StatusNotFound {
		t.Fatalf("preview permission bypassed resource/mod scope: %d %s", crossScopeResponse.Code, crossScopeResponse.Body.String())
	}

	var templateID, sectionID int64
	var sectionPublicID string
	if err = pool.QueryRow(ctx, `select id from mod_content_templates where builtin order by id limit 1`).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_sections(mod_id,version_id,template_id,default_locale,display_mode,status)
		values($1,$2,$3,'en-US','compact','active') returning id,public_id`,
		secondModID, versionID, templateID).Scan(&sectionID, &sectionPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_content_section_resources(section_id,version_id,resource_id,ordinal)
		values($1,$2,$3,0)`, sectionID, versionID, resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update mod_resource_version_details set status='active' where resource_id=$1 and version_id=$2`,
		resourceID, versionID); err != nil {
		t.Fatal(err)
	}

	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/mods/"+secondSlug+"/content-resources/"+resourcePublicID, nil)
	detailRequest.SetPathValue("siteId", secondSlug)
	detailRequest.SetPathValue("resourceId", resourcePublicID)
	detailResponse := httptest.NewRecorder()
	server.modContentResource(detailResponse, detailRequest)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("published detail returned %d: %s", detailResponse.Code, detailResponse.Body.String())
	}
	var payload struct {
		Data struct {
			Details []struct {
				VersionPublicID string `json:"versionPublicId"`
				SectionPublicID string `json:"sectionPublicId"`
			} `json:"details"`
		} `json:"data"`
	}
	if err = json.Unmarshal(detailResponse.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Details) != 1 ||
		payload.Data.Details[0].VersionPublicID != versionPublicID ||
		payload.Data.Details[0].SectionPublicID != sectionPublicID {
		t.Fatalf("detail did not preserve its current section: %s", detailResponse.Body.String())
	}
}

func TestModContentVersionArchiveAndResourceRevivalIntegration(t *testing.T) {
	pool := openModContentSafetyTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	suffix := time.Now().UnixNano() % 1_000_000
	var actorID int64
	if err = tx.QueryRow(ctx, `insert into users(username,email,display_name,password_hash,email_verified)
		values($1,$2,'Lifecycle test','test',true) returning id`,
		fmt.Sprintf("lifecycle-%06d", suffix), fmt.Sprintf("lifecycle-%06d@example.invalid", suffix)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	var modID, otherModID int64
	if err = tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,created_by)
		values($1,$2,'Lifecycle mod','approved',$3) returning id`,
		randomCatalogPublicID(), fmt.Sprintf("lifecycle-mod-%06d", suffix), actorID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,created_by)
		values($1,$2,'Other lifecycle mod','approved',$3) returning id`,
		randomCatalogPublicID(), fmt.Sprintf("lifecycle-other-%06d", suffix), actorID).Scan(&otherModID); err != nil {
		t.Fatal(err)
	}
	var versionID int64
	var versionPublicID string
	if err = tx.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status,created_by,updated_by)
		values($1,'1.21.1 / NeoForge',array['1.21.1'],array['neoforge'],'active',$2,$2) returning id,public_id`,
		modID, actorID).Scan(&versionID, &versionPublicID); err != nil {
		t.Fatal(err)
	}

	packageID, jobID, importRevisionID := newExportID(), newExportID(), newExportID()
	archiveHash := fmt.Sprintf("%064d", time.Now().UnixNano())
	if _, err = tx.Exec(ctx, `insert into catalog_import_packages(
		id,sha256,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest)
		values($1,$2,'lifecycle.zip','mcmods-export/v1','test','1.21.1','neoforge','{}')`,
		packageID, archiveHash); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_jobs(
		id,mod_id,package_id,target_version_id,importer_version,status,created_by)
		values($1,$2,$3,$4,$5,'ready',$6)`,
		jobID, modID, packageID, versionID, modExportImporterVersion, actorID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_revisions(
		id,mod_id,package_id,job_id,target_version_id,revision_no,status,minecraft_version,loader,
		exporter_version,source_namespace,is_active)
		values($1,$2,$3,$4,$5,1,'ready','1.21.1','neoforge','test','fixture',true)`,
		importRevisionID, modID, packageID, jobID, versionID); err != nil {
		t.Fatal(err)
	}

	resourcePublicID := randomCatalogPublicID()
	var resourceID int64
	if err = tx.QueryRow(ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status)
		values($1,$2,'resource','active') returning id`,
		fmt.Sprintf("resource:lifecycle:%06d", suffix), resourcePublicID).Scan(&resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved)
		values($1,'minecraft.item',$2,'lifecycle',$3,$4,true)`,
		resourceID, fmt.Sprintf("lifecycle:item_%06d", suffix), fmt.Sprintf("item_%06d", suffix), modID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into mod_resource_bindings(resource_id,mod_id) values($1,$2)`, resourceID, modID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into mod_resource_version_details(resource_id,version_id,default_locale,definition,status,created_by,updated_by)
		values($1,$2,'pt-BR','{"old":true}'::jsonb,'archived',$3,$3)`, resourceID, versionID, actorID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into mod_resource_version_detail_localizations(
		resource_id,version_id,locale,name,summary,content_markdown,provenance)
		values($1,$2,'pt-BR','Cobre','Resumo','Conteúdo','import')`, resourceID, versionID); err != nil {
		t.Fatal(err)
	}

	definition := []byte(`{"hardness":4}`)
	err = reserveModContentResourceDetailTx(ctx, tx, resourceID, versionID, otherModID, "en-US", definition, nil, nil, actorID)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("different mod revived archived detail: %v", err)
	}
	if err = reserveModContentResourceDetailTx(ctx, tx, resourceID, versionID, modID, "en-US", definition, nil, nil, actorID); err != nil {
		t.Fatalf("same binding could not revive archived detail: %v", err)
	}
	var detailStatus, defaultLocale string
	var publishedRevisionID *int64
	var storedDefinition []byte
	var localizationCount int
	if err = tx.QueryRow(ctx, `select status,default_locale,published_revision_id,definition
		from mod_resource_version_details where resource_id=$1 and version_id=$2`,
		resourceID, versionID).Scan(&detailStatus, &defaultLocale, &publishedRevisionID, &storedDefinition); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `select count(*)::int from mod_resource_version_detail_localizations
		where resource_id=$1 and version_id=$2`, resourceID, versionID).Scan(&localizationCount); err != nil {
		t.Fatal(err)
	}
	var decodedDefinition map[string]any
	if err = json.Unmarshal(storedDefinition, &decodedDefinition); err != nil {
		t.Fatalf("decode revived definition: %v", err)
	}
	if detailStatus != "pending" || defaultLocale != "en-US" || publishedRevisionID != nil || localizationCount != 0 ||
		decodedDefinition["hardness"] != float64(4) {
		t.Fatalf("archived detail was not reset safely: status=%s locale=%s revision=%v localizations=%d definition=%s",
			detailStatus, defaultLocale, publishedRevisionID, localizationCount, storedDefinition)
	}
	if err = reserveModContentResourceDetailTx(ctx, tx, resourceID, versionID, modID, "en-US", definition, nil, nil, actorID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("pending detail was reserved twice: %v", err)
	}

	var contentRevisionID int64
	if err = tx.QueryRow(ctx, `insert into content_revisions(
		entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by,source)
		select route.entity_type,route.internal_id,'mod_content_version',$1,1,'{}'::jsonb,$2,$3,'test'
		from public_routes route where route.public_id=$1 and route.entity_type='mod_content_version'
		returning id`, versionPublicID, fmt.Sprintf("archive-%06d", suffix), actorID).Scan(&contentRevisionID); err != nil {
		t.Fatal(err)
	}
	if err = publishModContentSnapshotTx(ctx, tx, contentRevisionID, modContentSnapshot{
		Kind:      "version",
		Operation: "delete",
		ModID:     modID,
		PublicID:  versionPublicID,
	}, actorID); err != nil {
		t.Fatal(err)
	}
	var versionStatus, importStatus string
	var importActive bool
	if err = tx.QueryRow(ctx, `select status from mod_content_versions where id=$1`, versionID).Scan(&versionStatus); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `select status,is_active from catalog_import_revisions where id=$1`,
		importRevisionID).Scan(&importStatus, &importActive); err != nil {
		t.Fatal(err)
	}
	if versionStatus != "archived" || importActive || importStatus != "superseded" {
		t.Fatalf("version archive left import public: version=%s import=%s active=%t", versionStatus, importStatus, importActive)
	}
}
