package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appconfig "mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestTEST040RatingPermissionsTransactionsTriggersVisibilityAndConcurrencyIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("TEST-040 exercises an isolated PostgreSQL rating state machine")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := newTEST040IsolatedDatabase(t, ctx)
	cfg := appconfig.Load()
	server := &Server{db: pool, cfg: cfg}

	var userID, authVersion int64
	var userPublicID string
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('test040-rater','test040-rater@example.test','not-used','active')
		returning id,public_id,auth_version`).Scan(&userID, &userPublicID, &authVersion); err != nil {
		t.Fatal(err)
	}
	claims, err := security.NewClaims(userPublicID, "test040-rater", "test040-rater@example.test", authVersion, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at)
		values($1,$2,$3,$4)`, security.SessionFingerprint(claims.SessionID), userID, authVersion, time.Unix(claims.ExpiresAt, 0)); err != nil {
		t.Fatal(err)
	}
	token, err := security.SignToken(cfg.JWTSecret, claims)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by) values
		('t040mod01','test040-rated','TEST-040 rated','approved',$1),
		('t040hid01','test040-hidden','TEST-040 hidden','pending',$1)`, userID); err != nil {
		t.Fatal(err)
	}
	var routeID int64
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and public_id='t040mod01'`).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
		create table test040_queue_mutations(
			object_route_id bigint primary key,inserts integer not null default 0,updates integer not null default 0
		);
		create function test040_count_queue_mutation() returns trigger language plpgsql as $$
		begin
			insert into test040_queue_mutations(object_route_id,inserts,updates)
			values(new.object_route_id,(tg_op='INSERT')::integer,(tg_op='UPDATE')::integer)
			on conflict(object_route_id) do update set
				inserts=test040_queue_mutations.inserts+excluded.inserts,
				updates=test040_queue_mutations.updates+excluded.updates;
			return new;
		end $$;
		create trigger test040_count_queue_mutation after insert or update on content_stats_refresh_queue
			for each row execute function test040_count_queue_mutation()
	`); err != nil {
		t.Fatal(err)
	}

	createHandler := server.requirePermission("rating.create", server.ratingItem)
	reviewHandler := server.requirePermission("rating.read", server.ratingReviews)
	validPayload := test040RatingPayload(t, 4)
	if response := performTEST040RatingRequest(t, ctx, token, createHandler, http.MethodPut, "mod", "t040mod01", validPayload); response.Code != http.StatusForbidden {
		t.Fatalf("create without permission status=%d body=%s", response.Code, response.Body.String())
	}
	if response := performTEST040RatingRequest(t, ctx, token, reviewHandler, http.MethodGet, "mod", "t040mod01", ""); response.Code != http.StatusForbidden {
		t.Fatalf("reviews without permission status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err = pool.Exec(ctx, `insert into user_permissions(user_id,permission_id,allow)
		select $1,id,true from permissions where code in ('rating.create','rating.read')`, userID); err != nil {
		t.Fatal(err)
	}
	for _, legacyType := range []string{"server", "minecraft-server", "resource-pack", "shader", "shader-pack"} {
		if response := performTEST040RatingRequest(t, ctx, token, createHandler, http.MethodPut, legacyType, "t040mod01", validPayload); response.Code != http.StatusNotFound {
			t.Fatalf("legacy rating type %q status=%d body=%s", legacyType, response.Code, response.Body.String())
		}
	}
	if response := performTEST040RatingRequest(t, ctx, token, createHandler, http.MethodPut, "mod", "t040hid01", validPayload); response.Code != http.StatusNotFound {
		t.Fatalf("pending target rating status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err = pool.Exec(ctx, `delete from content_stats_refresh_queue where object_route_id=$1`, routeID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `delete from test040_queue_mutations where object_route_id=$1`, routeID); err != nil {
		t.Fatal(err)
	}
	if response := performTEST040RatingRequest(t, ctx, token, createHandler, http.MethodPut, "mod", "t040mod01", validPayload); response.Code != http.StatusOK {
		t.Fatalf("create rating status=%d body=%s", response.Code, response.Body.String())
	}
	assertTEST040RatingFacts(t, ctx, pool, routeID, 1, 4)
	assertTEST040QueueMutations(t, ctx, pool, routeID, 1, 0)
	if _, err = pool.Exec(ctx, `select refresh_content_popularity($1)`, routeID); err != nil {
		t.Fatal(err)
	}
	var projectedCount int
	if err = pool.QueryRow(ctx, `select rating_count from content_popularity_stats where object_route_id=$1`, routeID).Scan(&projectedCount); err != nil {
		t.Fatal(err)
	}
	if projectedCount != 1 {
		t.Fatalf("rating projection count=%d want=1", projectedCount)
	}
	summary := performTEST040RatingRequest(t, ctx, token, server.optionalAuth(server.ratingSummary), http.MethodGet, "mod", "t040mod01", "")
	if summary.Code != http.StatusOK || !strings.Contains(summary.Body.String(), `"ratingCount":1`) ||
		!strings.Contains(summary.Body.String(), `"canRate":true`) || !strings.Contains(summary.Body.String(), `"canViewReviews":true`) ||
		!strings.Contains(summary.Body.String(), `"myRating"`) {
		t.Fatalf("rating summary status=%d body=%s", summary.Code, summary.Body.String())
	}
	if reviews := performTEST040RatingRequest(t, ctx, token, reviewHandler, http.MethodGet, "mod", "t040mod01", ""); reviews.Code != http.StatusOK || !strings.Contains(reviews.Body.String(), `"items":[{`) {
		t.Fatalf("rating reviews status=%d body=%s", reviews.Code, reviews.Body.String())
	}

	if _, err = pool.Exec(ctx, `delete from test040_queue_mutations where object_route_id=$1`, routeID); err != nil {
		t.Fatal(err)
	}
	if response := performTEST040RatingRequest(t, ctx, token, createHandler, http.MethodPut, "mod", "t040mod01", test040RatingPayload(t, 3)); response.Code != http.StatusOK {
		t.Fatalf("update rating status=%d body=%s", response.Code, response.Body.String())
	}
	assertTEST040RatingFacts(t, ctx, pool, routeID, 1, 3)
	assertTEST040QueueMutations(t, ctx, pool, routeID, 0, 1)

	if _, err = pool.Exec(ctx, `
		create function test040_reject_score() returns trigger language plpgsql as $$
		begin
			if new.dimension_code='content' and new.score=1 then raise exception 'TEST040 score persistence rejected'; end if;
			return new;
		end $$;
		create trigger test040_reject_score before insert on content_rating_scores
			for each row execute function test040_reject_score()
	`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `delete from test040_queue_mutations where object_route_id=$1`, routeID); err != nil {
		t.Fatal(err)
	}
	if response := performTEST040RatingRequest(t, ctx, token, createHandler, http.MethodPut, "mod", "t040mod01", test040RatingPayload(t, 1)); response.Code != http.StatusInternalServerError {
		t.Fatalf("score failure status=%d body=%s", response.Code, response.Body.String())
	}
	assertTEST040RatingFacts(t, ctx, pool, routeID, 1, 3)
	assertTEST040QueueMutations(t, ctx, pool, routeID, 0, 0)
	if _, err = pool.Exec(ctx, `drop trigger test040_reject_score on content_rating_scores;
		drop function test040_reject_score()`); err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, `update mods set review_status='pending' where project_code='t040mod01'`); err != nil {
		t.Fatal(err)
	}
	if response := performTEST040RatingRequest(t, ctx, token, createHandler, http.MethodPut, "mod", "t040mod01", validPayload); response.Code != http.StatusNotFound {
		t.Fatalf("hidden target update status=%d body=%s", response.Code, response.Body.String())
	}
	if response := performTEST040RatingRequest(t, ctx, token, reviewHandler, http.MethodGet, "mod", "t040mod01", ""); response.Code != http.StatusNotFound {
		t.Fatalf("hidden target reviews status=%d body=%s", response.Code, response.Body.String())
	}
	if response := performTEST040RatingRequest(t, ctx, token, server.optionalAuth(server.ratingSummary), http.MethodGet, "mod", "t040mod01", ""); response.Code != http.StatusNotFound {
		t.Fatalf("hidden target summary status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err = pool.Exec(ctx, `update mods set review_status='approved' where project_code='t040mod01'`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `delete from content_stats_refresh_queue where object_route_id=$1`, routeID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `delete from test040_queue_mutations where object_route_id=$1`, routeID); err != nil {
		t.Fatal(err)
	}

	codes := make(chan int, 2)
	concurrentPayloads := []string{test040RatingPayload(t, 2), test040RatingPayload(t, 5)}
	for _, payload := range concurrentPayloads {
		payload := payload
		go func() {
			request := httptest.NewRequest(http.MethodPut, "/api/v1/ratings/mod/t040mod01", strings.NewReader(payload)).WithContext(
				context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}),
			)
			request.Header.Set("Content-Type", "application/json")
			request.SetPathValue("targetType", "mod")
			request.SetPathValue("publicId", "t040mod01")
			response := httptest.NewRecorder()
			server.ratingItem(response, request)
			codes <- response.Code
		}()
	}
	for range 2 {
		if code := <-codes; code != http.StatusOK {
			t.Fatalf("concurrent rating status=%d", code)
		}
	}
	var overall, minimumScore, maximumScore, scoreCount int
	if err = pool.QueryRow(ctx, `select rating.overall_score,count(score.*),min(score.score),max(score.score)
		from content_ratings rating join content_rating_scores score on score.rating_id=rating.id
		where rating.object_route_id=$1 and rating.author_id=$2 group by rating.id`, routeID, userID).
		Scan(&overall, &scoreCount, &minimumScore, &maximumScore); err != nil {
		t.Fatal(err)
	}
	if scoreCount != len(ratingDimensions["mod"]) || minimumScore != maximumScore || overall != minimumScore || overall != 2 && overall != 5 {
		t.Fatalf("concurrent final rating overall/count/min/max=%d/%d/%d/%d", overall, scoreCount, minimumScore, maximumScore)
	}
	assertTEST040QueueMutations(t, ctx, pool, routeID, 1, 1)
	var queuedRows int
	if err = pool.QueryRow(ctx, `select count(*) from content_stats_refresh_queue where object_route_id=$1
		and refresh_metrics and refresh_popularity`, routeID).Scan(&queuedRows); err != nil {
		t.Fatal(err)
	}
	if queuedRows != 1 {
		t.Fatalf("coalesced refresh queue rows=%d want=1", queuedRows)
	}

	if _, err = pool.Exec(ctx, `delete from test040_queue_mutations where object_route_id=$1`, routeID); err != nil {
		t.Fatal(err)
	}
	if response := performTEST040RatingRequest(t, ctx, token, createHandler, http.MethodDelete, "mod", "t040mod01", ""); response.Code != http.StatusNoContent {
		t.Fatalf("delete rating status=%d body=%s", response.Code, response.Body.String())
	}
	assertTEST040RatingFacts(t, ctx, pool, routeID, 0, 0)
	assertTEST040QueueMutations(t, ctx, pool, routeID, 0, 1)
	if _, err = pool.Exec(ctx, `select refresh_content_popularity($1)`, routeID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select rating_count from content_popularity_stats where object_route_id=$1`, routeID).Scan(&projectedCount); err != nil {
		t.Fatal(err)
	}
	if projectedCount != 0 {
		t.Fatalf("rating projection after delete=%d want=0", projectedCount)
	}
}

func test040RatingPayload(t *testing.T, score int) string {
	t.Helper()
	scores := make(map[string]int, len(ratingDimensions["mod"]))
	for _, dimension := range ratingDimensions["mod"] {
		scores[dimension.Code] = score
	}
	encoded, err := json.Marshal(ratingUpsertRequest{OverallScore: score, Scores: scores, Message: fmt.Sprintf("TEST-040 score %d", score)})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func performTEST040RatingRequest(
	t *testing.T,
	ctx context.Context,
	token string,
	handler http.HandlerFunc,
	method, targetType, publicID, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/api/v1/ratings/"+targetType+"/"+publicID, strings.NewReader(body)).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("targetType", targetType)
	request.SetPathValue("publicId", publicID)
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func assertTEST040RatingFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, routeID int64, ratingCount, score int) {
	t.Helper()
	var ratings, scores, minimumScore, maximumScore int
	if err := pool.QueryRow(ctx, `select count(*),coalesce(min(overall_score),0),coalesce(max(overall_score),0)
		from content_ratings where object_route_id=$1`, routeID).Scan(&ratings, &minimumScore, &maximumScore); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from content_rating_scores score
		join content_ratings rating on rating.id=score.rating_id where rating.object_route_id=$1`, routeID).Scan(&scores); err != nil {
		t.Fatal(err)
	}
	if ratings != ratingCount || minimumScore != score || maximumScore != score || scores != ratingCount*len(ratingDimensions["mod"]) {
		t.Fatalf("rating facts count/min/max/scores=%d/%d/%d/%d want=%d/%d/%d", ratings, minimumScore, maximumScore, scores, ratingCount, score, ratingCount*len(ratingDimensions["mod"]))
	}
}

func assertTEST040QueueMutations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, routeID int64, wantInserts, wantUpdates int) {
	t.Helper()
	var inserts, updates int
	if err := pool.QueryRow(ctx, `select coalesce(sum(inserts),0),coalesce(sum(updates),0)
		from test040_queue_mutations where object_route_id=$1`, routeID).Scan(&inserts, &updates); err != nil {
		t.Fatal(err)
	}
	if inserts != wantInserts || updates != wantUpdates {
		t.Fatalf("refresh queue mutations insert/update=%d/%d want=%d/%d", inserts, updates, wantInserts, wantUpdates)
	}
}

func newTEST040IsolatedDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	baseConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("test040_rating_%d", time.Now().UnixNano())
	namePattern := regexp.MustCompile(`^test040_rating_[0-9]+$`)
	if !namePattern.MatchString(databaseName) {
		adminPool.Close()
		t.Fatalf("refuse unsafe TEST040 database name %q", databaseName)
	}
	quotedName := pgx.Identifier{databaseName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create database "+quotedName); err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if !namePattern.MatchString(databaseName) {
			t.Errorf("refuse unsafe TEST040 cleanup database name %q", databaseName)
		} else if _, dropErr := adminPool.Exec(cleanupCtx, "drop database "+quotedName+" with (force)"); dropErr != nil {
			t.Errorf("drop isolated TEST040 database: %v", dropErr)
		}
		adminPool.Close()
	})
	scopedConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.ConnConfig.Database = databaseName
	scopedConfig.MaxConns, scopedConfig.MinConns = 8, 1
	pool, err = pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}
