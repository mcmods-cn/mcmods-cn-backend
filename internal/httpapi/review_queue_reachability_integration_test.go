package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestContentReviewQueueTraversesAndSearchesBeyondLegacyCutoffIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify review queue reachability")
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
		values('queue_submitter','queue-submitter@example.invalid','test-only',true) returning id`).Scan(&submitterID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('queue_reviewer','queue-reviewer@example.invalid','test-only',true) returning id`).Scan(&reviewerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('queue0001','queue-scale','Queue scale','approved',$1) returning id`, submitterID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into content_revisions(
		entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by,source,created_at)
		select 'mod',$1,'mod','queue-'||lpad(series::text,5,'0'),series,
			jsonb_build_object('series',series),'queue-hash-'||series::text,$2,'test',
			timestamptz '2026-01-01 00:00:00+00'+series*interval '1 second'
		from generate_series(1,2005) series`, modID, submitterID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into change_requests(
		entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,reason,submitted_by,submitted_at)
		select revision.entity_type,revision.entity_id,revision.aggregate_type,revision.aggregate_key,revision.id,
			'pending','reason '||revision.revision_no::text,$1,revision.created_at
		from content_revisions revision where revision.aggregate_type='mod' and revision.aggregate_key like 'queue-%'`, submitterID); err != nil {
		t.Fatal(err)
	}

	type queueResponse struct {
		Items  []modContentReviewItem `json:"items"`
		Total  int64                  `json:"total"`
		Facets struct {
			Categories []struct {
				Value string `json:"value"`
				Count int64  `json:"count"`
			} `json:"categories"`
			Operations []struct {
				Value string `json:"value"`
				Count int64  `json:"count"`
			} `json:"operations"`
			ProjectTypes []struct {
				Value string `json:"value"`
				Count int64  `json:"count"`
			} `json:"projectTypes"`
		} `json:"facets"`
	}
	requestQueue := func(claims security.Claims, target string) queueResponse {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, claims))
		response := httptest.NewRecorder()
		(&Server{db: pool}).adminModContentReviews(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("queue %s returned %d: %s", target, response.Code, response.Body.String())
		}
		var envelope struct {
			Data queueResponse `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}
	globalClaims := security.Claims{Subject: reviewerID, PermissionRules: []security.PermissionRule{
		{Code: "content.review", Allow: true, Priority: 100},
	}}
	if !canReviewAllContent(globalClaims) || !canReviewAllProjects(globalClaims) {
		t.Fatalf("global review claims were not resolved: %#v", globalClaims)
	}
	var revisionCount, requestCount, branchCount int
	if err = pool.QueryRow(ctx, `select
		(select count(*)::int from content_revisions where aggregate_type='mod' and aggregate_key like 'queue-%'),
		(select count(*)::int from change_requests where aggregate_type='mod' and aggregate_key like 'queue-%' and status='pending'),
		(select count(*)::int from content_revisions revision
		 join change_requests request on request.proposed_revision_id=revision.id
		 join mods mod on mod.id=revision.entity_id
		 where revision.aggregate_type='mod' and request.status='pending')`).Scan(&revisionCount, &requestCount, &branchCount); err != nil {
		t.Fatal(err)
	}
	if revisionCount != 2005 || requestCount != 2005 || branchCount != 2005 {
		t.Fatalf("fixture revisions=%d requests=%d branch=%d; want 2005 each", revisionCount, requestCount, branchCount)
	}

	lastPage := requestQueue(globalClaims, "/api/v1/reviews/content?limit=100&offset=2000")
	if lastPage.Total != 2005 || len(lastPage.Items) != 5 {
		t.Fatalf("last page total=%d items=%d; want 2005 and 5", lastPage.Total, len(lastPage.Items))
	}
	if !strings.HasSuffix(lastPage.Items[0].Title, "#2001") || !strings.HasSuffix(lastPage.Items[4].Title, "#2005") {
		t.Fatalf("last page first=%q last=%q", lastPage.Items[0].Title, lastPage.Items[4].Title)
	}
	if lastPage.Items[4].ReviewerScope != "global" ||
		!strings.HasPrefix(lastPage.Items[4].ReviewURL, "/api/v1/mods/queue-scale/revisions/") {
		t.Fatalf("last item scope=%q reviewURL=%q", lastPage.Items[4].ReviewerScope, lastPage.Items[4].ReviewURL)
	}
	if len(lastPage.Facets.Categories) != 1 || lastPage.Facets.Categories[0].Value != "mod" || lastPage.Facets.Categories[0].Count != 2005 ||
		len(lastPage.Facets.Operations) != 1 || lastPage.Facets.Operations[0].Value != "create" || lastPage.Facets.Operations[0].Count != 2005 ||
		len(lastPage.Facets.ProjectTypes) != 1 || lastPage.Facets.ProjectTypes[0].Value != "mod" || lastPage.Facets.ProjectTypes[0].Count != 2005 {
		t.Fatalf("unexpected full-queue facets: %#v", lastPage.Facets)
	}

	tailSearch := requestQueue(globalClaims, "/api/v1/reviews/content?q=revision+%232005&limit=10&offset=0")
	if tailSearch.Total != 1 || len(tailSearch.Items) != 1 || !strings.HasSuffix(tailSearch.Items[0].Title, "#2005") {
		t.Fatalf("tail search total=%d items=%#v", tailSearch.Total, tailSearch.Items)
	}

	projectClaims := security.Claims{Subject: reviewerID, PermissionRules: []security.PermissionRule{
		{Code: "project.review.queue0001", Allow: true, Priority: 100},
	}}
	projectTail := requestQueue(projectClaims, "/api/v1/reviews/content?q=revision+%232005&limit=10")
	if projectTail.Total != 1 || len(projectTail.Items) != 1 || projectTail.Items[0].ReviewerScope != "project" {
		t.Fatalf("project-scoped tail result: %#v", projectTail)
	}
	selfClaims := security.Claims{Subject: submitterID, PermissionRules: projectClaims.PermissionRules}
	selfQueue := requestQueue(selfClaims, "/api/v1/reviews/content?limit=10")
	if selfQueue.Total != 0 || len(selfQueue.Items) != 0 {
		t.Fatalf("project-scoped submitter saw own queue: total=%d items=%d", selfQueue.Total, len(selfQueue.Items))
	}
}
