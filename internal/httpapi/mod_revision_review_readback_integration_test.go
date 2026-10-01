package httpapi

import (
	"bytes"
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
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestModRevisionReviewReadbackFailureRollsBackIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify atomic review readback")
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

	var submitterID, reviewerID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('readback_submitter','readback-submitter@example.invalid','test-only',true) returning id`).Scan(&submitterID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('readback_reviewer','readback-reviewer@example.invalid','test-only',true) returning id`).Scan(&reviewerID); err != nil {
		t.Fatal(err)
	}

	type fixture struct {
		projectID       string
		revisionID      string
		changeRequestID int64
	}
	createFixture := func(projectID, slug, snapshot string) fixture {
		t.Helper()
		var modID, revisionInternalID int64
		var result fixture
		result.projectID = projectID
		if insertErr := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
			values($1,$2,$2,'approved',$3) returning id`, projectID, slug, submitterID).Scan(&modID); insertErr != nil {
			t.Fatal(insertErr)
		}
		if insertErr := pool.QueryRow(ctx, `insert into content_revisions(
			entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by,source)
			values('mod',$1,'mod',$2,1,$3::jsonb,$2||'-hash',$4,'test') returning id,public_id`,
			modID, projectID, snapshot, submitterID).Scan(&revisionInternalID, &result.revisionID); insertErr != nil {
			t.Fatal(insertErr)
		}
		if insertErr := pool.QueryRow(ctx, `insert into change_requests(
			entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,reason,submitted_by)
			values('mod',$1,'mod',$2,$3,'pending','readback fixture',$4) returning id`,
			modID, projectID, revisionInternalID, submitterID).Scan(&result.changeRequestID); insertErr != nil {
			t.Fatal(insertErr)
		}
		return result
	}
	broken := createFixture("readback1", "readback-broken", `[]`)
	valid := createFixture("readback2", "readback-valid", `{"siteId":"readback-valid","primaryName":"Valid"}`)
	// Valid post-commit security refreshes need the same disabled-cache contract
	// as NewServer; a nil cache deliberately reports an unavailable dependency.
	cache := querycache.New(config.RedisConfig{})
	defer cache.Close()
	server := &Server{db: pool, cache: cache}
	review := func(target fixture) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/mods/revisions/"+target.revisionID,
			bytes.NewBufferString(`{"status":"rejected","note":"readback check"}`))
		request.SetPathValue("revisionId", target.revisionID)
		request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{
			Subject: reviewerID,
			PermissionRules: []security.PermissionRule{
				{Code: "project.review." + target.projectID, Allow: true, Priority: 100},
			},
		}))
		response := httptest.NewRecorder()
		server.reviewModRevision(response, request)
		return response
	}

	brokenResponse := review(broken)
	if brokenResponse.Code != http.StatusInternalServerError {
		t.Fatalf("readback decode failure returned %d: %s", brokenResponse.Code, brokenResponse.Body.String())
	}
	var brokenStatus string
	var brokenEvents int
	if err = pool.QueryRow(ctx, `select request.status,
		(select count(*)::int from review_events event where event.change_request_id=request.id and event.event_type='rejected')
		from change_requests request where request.id=$1`, broken.changeRequestID).Scan(&brokenStatus, &brokenEvents); err != nil {
		t.Fatal(err)
	}
	if brokenStatus != "pending" || brokenEvents != 0 {
		t.Fatalf("failed readback committed status=%q rejectedEvents=%d", brokenStatus, brokenEvents)
	}

	validResponse := review(valid)
	if validResponse.Code != http.StatusOK {
		t.Fatalf("valid review returned %d: %s", validResponse.Code, validResponse.Body.String())
	}
	var envelope struct {
		Data modRevisionResponse `json:"data"`
	}
	if err = json.Unmarshal(validResponse.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ID != valid.revisionID || envelope.Data.Status != "rejected" {
		t.Fatalf("invalid committed response: %#v", envelope.Data)
	}
	var validStatus string
	var validEvents int
	if err = pool.QueryRow(ctx, `select request.status,
		(select count(*)::int from review_events event where event.change_request_id=request.id and event.event_type='rejected')
		from change_requests request where request.id=$1`, valid.changeRequestID).Scan(&validStatus, &validEvents); err != nil {
		t.Fatal(err)
	}
	if validStatus != "rejected" || validEvents != 1 {
		t.Fatalf("valid review status=%q rejectedEvents=%d", validStatus, validEvents)
	}
}
