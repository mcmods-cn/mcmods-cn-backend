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
	"mcmods-cn-backend/internal/security"
)

func TestProjectRevisionHistoriesHonorExactReviewScopeIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify project-scoped revision visibility")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
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
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var submitterID, reviewerID, modID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('history_submitter','history-submitter@example.invalid','test-only',true) returning id`).Scan(&submitterID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('history_reviewer','history-reviewer@example.invalid','test-only',true) returning id`).Scan(&reviewerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('review001','review-visibility','Review visibility','approved',$1) returning id`, submitterID).Scan(&modID); err != nil {
		t.Fatal(err)
	}

	var approvedID, pendingID int64
	var approvedPublicID, pendingPublicID string
	if err = pool.QueryRow(ctx, `insert into content_revisions(
		entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by,source)
		values('mod',$1,'mod','review001',1,
			'{"siteId":"review-visibility","primaryName":"Approved"}'::jsonb,'history-approved',$2,'test')
		returning id,public_id`, modID, submitterID).Scan(&approvedID, &approvedPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into change_requests(
		entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,reason,submitted_by)
		values('mod',$1,'mod','review001',$2,'approved','approved fixture',$3)`, modID, approvedID, submitterID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into content_revisions(
		entity_type,entity_id,aggregate_type,aggregate_key,revision_no,base_revision_id,snapshot,snapshot_hash,created_by,source)
		values('mod',$1,'mod','review001',2,$2,
			'{"siteId":"review-visibility","primaryName":"Pending"}'::jsonb,'history-pending',$3,'test')
		returning id,public_id`, modID, approvedID, submitterID).Scan(&pendingID, &pendingPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into change_requests(
		entity_type,entity_id,aggregate_type,aggregate_key,base_revision_id,proposed_revision_id,status,reason,submitted_by)
		values('mod',$1,'mod','review001',$2,$3,'pending','pending fixture',$4)`,
		modID, approvedID, pendingID, submitterID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update mods set published_revision_id=$1 where id=$2`, approvedID, modID); err != nil {
		t.Fatal(err)
	}

	insertRevisionPair := func(entityType, aggregateType, aggregateKey string, entityID int64) (int64, string, string) {
		t.Helper()
		var baseID, proposedID int64
		var basePublicID, proposedPublicID string
		if insertErr := pool.QueryRow(ctx, `insert into content_revisions(
			entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by,source)
			values($1,$2,$3,$4,1,jsonb_build_object('version',1),$4||'-approved',$5,'test')
			returning id,public_id`, entityType, entityID, aggregateType, aggregateKey, submitterID).
			Scan(&baseID, &basePublicID); insertErr != nil {
			t.Fatal(insertErr)
		}
		if _, insertErr := pool.Exec(ctx, `insert into change_requests(
			entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,reason,submitted_by)
			values($1,$2,$3,$4,$5,'approved','approved fixture',$6)`,
			entityType, entityID, aggregateType, aggregateKey, baseID, submitterID); insertErr != nil {
			t.Fatal(insertErr)
		}
		if insertErr := pool.QueryRow(ctx, `insert into content_revisions(
			entity_type,entity_id,aggregate_type,aggregate_key,revision_no,base_revision_id,snapshot,snapshot_hash,created_by,source)
			values($1,$2,$3,$4,2,$5,jsonb_build_object('version',2),$4||'-pending',$6,'test')
			returning id,public_id`, entityType, entityID, aggregateType, aggregateKey, baseID, submitterID).
			Scan(&proposedID, &proposedPublicID); insertErr != nil {
			t.Fatal(insertErr)
		}
		if _, insertErr := pool.Exec(ctx, `insert into change_requests(
			entity_type,entity_id,aggregate_type,aggregate_key,base_revision_id,proposed_revision_id,status,reason,submitted_by)
			values($1,$2,$3,$4,$5,$6,'pending','pending fixture',$7)`,
			entityType, entityID, aggregateType, aggregateKey, baseID, proposedID, submitterID); insertErr != nil {
			t.Fatal(insertErr)
		}
		return baseID, basePublicID, proposedPublicID
	}

	var modpackID int64
	var modpackPublicID string
	if err = pool.QueryRow(ctx, `insert into modpacks(slug,primary_name,review_status,submitted_by)
		values('review-pack','Review pack','approved',$1) returning id,public_id`, submitterID).
		Scan(&modpackID, &modpackPublicID); err != nil {
		t.Fatal(err)
	}
	modpackBaseID, modpackBasePublicID, modpackPendingPublicID := insertRevisionPair("modpack", modpackAggregate, modpackPublicID, modpackID)
	if _, err = pool.Exec(ctx, `update modpacks set published_revision_id=$1 where id=$2`, modpackBaseID, modpackID); err != nil {
		t.Fatal(err)
	}

	var simpleProjectID int64
	var simpleProjectPublicID string
	if err = pool.QueryRow(ctx, `insert into simple_projects(project_type,slug,primary_name,review_status,submitted_by)
		values('plugin','review-plugin','Review plugin','approved',$1) returning id,public_id`, submitterID).
		Scan(&simpleProjectID, &simpleProjectPublicID); err != nil {
		t.Fatal(err)
	}
	simpleBaseID, simpleBasePublicID, simplePendingPublicID := insertRevisionPair("plugin", simpleProjectAggregate, simpleProjectPublicID, simpleProjectID)
	if _, err = pool.Exec(ctx, `update simple_projects set published_revision_id=$1 where id=$2`, simpleBaseID, simpleProjectID); err != nil {
		t.Fatal(err)
	}

	var modRouteID, changelogID, changelogRevisionID int64
	var changelogPublicID string
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, modID).Scan(&modRouteID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into project_changelogs(
		object_route_id,event_at,minecraft_versions,project_version,default_locale,review_status,created_by)
		values($1,now(),array['1.21.1'],'1.0.0','zh-CN','pending',$2) returning id,public_id`, modRouteID, submitterID).
		Scan(&changelogID, &changelogPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into content_revisions(
		entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by,source)
		values('project_changelog',$1,$2,$3,1,'{}'::jsonb,$3||'-pending',$4,'test') returning id`,
		changelogID, projectChangelogAggregate, changelogPublicID, submitterID).Scan(&changelogRevisionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into change_requests(
		entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,reason,submitted_by)
		values('project_changelog',$1,$2,$3,$4,'pending','pending changelog fixture',$5)`,
		changelogID, projectChangelogAggregate, changelogPublicID, changelogRevisionID, submitterID); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	history := func(claims security.Claims) ([]modRevisionResponse, int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/mods/review-visibility/revisions", nil)
		request.SetPathValue("siteId", "review-visibility")
		request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, claims))
		response := httptest.NewRecorder()
		server.modRevisionHistory(response, request)
		if response.Code != http.StatusOK {
			return nil, response.Code
		}
		var envelope struct {
			Data struct {
				Items []modRevisionResponse `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data.Items, response.Code
	}
	compare := func(claims security.Claims) int {
		t.Helper()
		target := "/api/v1/mods/review-visibility/revisions/compare?before=" + approvedPublicID + "&after=" + pendingPublicID
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.SetPathValue("siteId", "review-visibility")
		request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, claims))
		response := httptest.NewRecorder()
		server.compareModRevisions(response, request)
		return response.Code
	}
	contentHistory := func(handler func(http.ResponseWriter, *http.Request), target string, pathValues map[string]string, claims security.Claims) ([]contentHistoryItem, int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		for key, value := range pathValues {
			request.SetPathValue(key, value)
		}
		request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, claims))
		response := httptest.NewRecorder()
		handler(response, request)
		if response.Code != http.StatusOK {
			return nil, response.Code
		}
		var envelope struct {
			Data struct {
				Items []contentHistoryItem `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data.Items, response.Code
	}

	exactRules := []security.PermissionRule{{Code: "project.review.review001", Allow: true, Priority: 100}}
	exactReviewer := security.Claims{Subject: reviewerID, PermissionRules: exactRules}
	items, status := history(exactReviewer)
	if status != http.StatusOK || len(items) != 2 || items[0].ID != pendingPublicID || items[1].ID != approvedPublicID {
		t.Fatalf("exact project reviewer history status=%d items=%#v", status, items)
	}
	if status = compare(exactReviewer); status != http.StatusOK {
		t.Fatalf("exact project reviewer comparison returned %d", status)
	}

	selfReviewer := security.Claims{Subject: submitterID, PermissionRules: exactRules}
	items, status = history(selfReviewer)
	if status != http.StatusOK || len(items) != 1 || items[0].ID != approvedPublicID {
		t.Fatalf("self reviewer history status=%d items=%#v", status, items)
	}
	if status = compare(selfReviewer); status != http.StatusForbidden {
		t.Fatalf("self reviewer comparison returned %d; want 403", status)
	}

	wrongProject := security.Claims{Subject: reviewerID, PermissionRules: []security.PermissionRule{
		{Code: "project.review.other0001", Allow: true, Priority: 100},
	}}
	items, status = history(wrongProject)
	if status != http.StatusOK || len(items) != 1 || items[0].ID != approvedPublicID {
		t.Fatalf("cross-project reviewer history status=%d items=%#v", status, items)
	}
	if status = compare(wrongProject); status != http.StatusForbidden {
		t.Fatalf("cross-project reviewer comparison returned %d; want 403", status)
	}

	globalReviewer := security.Claims{Subject: submitterID, PermissionRules: []security.PermissionRule{
		{Code: "project.review", Allow: true, Priority: 100},
	}}
	items, status = history(globalReviewer)
	if status != http.StatusOK || len(items) != 2 {
		t.Fatalf("global reviewer history status=%d items=%#v", status, items)
	}
	if status = compare(globalReviewer); status != http.StatusOK {
		t.Fatalf("global reviewer comparison returned %d", status)
	}

	assertSharedHistory := func(label, projectID, target string, pathValues map[string]string,
		handler func(http.ResponseWriter, *http.Request), basePublicID, proposedPublicID string) {
		t.Helper()
		rules := []security.PermissionRule{{Code: "project.review." + projectID, Allow: true, Priority: 100}}
		exact := security.Claims{Subject: reviewerID, PermissionRules: rules}
		historyItems, historyStatus := contentHistory(handler, target, pathValues, exact)
		if historyStatus != http.StatusOK || len(historyItems) != 2 ||
			historyItems[0].ID != proposedPublicID || historyItems[1].ID != basePublicID {
			t.Fatalf("%s exact reviewer history status=%d items=%#v", label, historyStatus, historyItems)
		}
		self := security.Claims{Subject: submitterID, PermissionRules: rules}
		historyItems, historyStatus = contentHistory(handler, target, pathValues, self)
		if historyStatus != http.StatusOK || len(historyItems) != 1 || historyItems[0].ID != basePublicID {
			t.Fatalf("%s self reviewer history status=%d items=%#v", label, historyStatus, historyItems)
		}
		cross := security.Claims{Subject: reviewerID, PermissionRules: []security.PermissionRule{
			{Code: "project.review.other0001", Allow: true, Priority: 100},
		}}
		historyItems, historyStatus = contentHistory(handler, target, pathValues, cross)
		if historyStatus != http.StatusOK || len(historyItems) != 1 || historyItems[0].ID != basePublicID {
			t.Fatalf("%s cross-project history status=%d items=%#v", label, historyStatus, historyItems)
		}
	}
	assertSharedHistory("modpack", modpackPublicID, "/api/v1/modpacks/review-pack/history",
		map[string]string{"siteId": "review-pack"}, server.modpackHistory, modpackBasePublicID, modpackPendingPublicID)
	assertSharedHistory("simple project", simpleProjectPublicID, "/api/v1/content-projects/plugin/review-plugin/history",
		map[string]string{"projectType": "plugin", "siteId": "review-plugin"}, server.simpleProjectHistory, simpleBasePublicID, simplePendingPublicID)

	changelogTarget := "/api/v1/changelogs/" + changelogPublicID + "/history"
	changelogPath := map[string]string{"id": changelogPublicID}
	changelogItems, changelogStatus := contentHistory(server.projectChangelogHistory, changelogTarget, changelogPath, exactReviewer)
	if changelogStatus != http.StatusOK || len(changelogItems) != 1 {
		t.Fatalf("exact project reviewer changelog history status=%d items=%#v", changelogStatus, changelogItems)
	}
	changelogItems, changelogStatus = contentHistory(server.projectChangelogHistory, changelogTarget, changelogPath, selfReviewer)
	if changelogStatus != http.StatusOK || len(changelogItems) != 0 {
		t.Fatalf("self reviewer changelog history status=%d items=%#v", changelogStatus, changelogItems)
	}
	if _, changelogStatus = contentHistory(server.projectChangelogHistory, changelogTarget, changelogPath, wrongProject); changelogStatus != http.StatusNotFound {
		t.Fatalf("cross-project reviewer changelog history returned %d; want 404", changelogStatus)
	}
}
