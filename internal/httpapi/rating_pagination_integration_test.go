package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

type ratingReviewHandlerPage struct {
	Items []struct {
		ID string `json:"id"`
	} `json:"items"`
	Limit      int    `json:"limit"`
	HasMore    bool   `json:"hasMore"`
	NextCursor string `json:"nextCursor"`
}

func TestRatingReviewHandlerBoundsMillionRowsWithStableCursorIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify rating pagination")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	poolConfig.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err = pool.Exec(ctx, `create temporary table public_routes(
		id bigint primary key,entity_type text not null,public_id text not null,internal_id bigint not null);
	create temporary table mods(id bigint primary key,review_status text not null);
	create temporary table users(
		id bigint primary key,public_id text not null,username text not null,avatar_url text not null);
	create temporary table content_ratings(
		id bigint primary key,public_id text not null,object_route_id bigint not null,author_id bigint not null,
		overall_score integer not null,message text not null,status text not null,
		created_at timestamptz not null,updated_at timestamptz not null);
	create index idx_content_ratings_target
		on content_ratings(object_route_id,status,updated_at desc,id desc);
	create temporary table content_rating_scores(
		rating_id bigint not null,dimension_code text not null,score integer not null);
	create temporary table system_settings(key text primary key,value jsonb not null)`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into public_routes values(77,'mod','perf05001',9);
		insert into mods values(9,'approved');
		insert into users values(1,'author001','Scale author','')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into content_ratings
		select value,'r'||lpad(value::text,8,'0'),77,1,1+(value%5)::integer,
			'rating '||value,'published',$1::timestamptz+value*interval '1 microsecond',
			$1::timestamptz+value*interval '1 microsecond'
		from generate_series(1,1000000) value`, base); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `analyze content_ratings; analyze users`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	load := func(cursor string) ratingReviewHandlerPage {
		t.Helper()
		path := "/api/v1/ratings/mod/perf05001/reviews?limit=20"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		counter.queries.Store(0)
		request := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		request.SetPathValue("targetType", "mod")
		request.SetPathValue("publicId", "perf05001")
		response := httptest.NewRecorder()
		server.ratingReviews(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		if queries := counter.queries.Load(); queries != 5 {
			t.Fatalf("rating review queries=%d want=5 (target=2,page=1,scores=1,settings=1)", queries)
		}
		if response.Body.Len() > 128<<10 {
			t.Fatalf("rating review page is unexpectedly large: %d bytes", response.Body.Len())
		}
		var envelope struct {
			Data ratingReviewHandlerPage `json:"data"`
		}
		if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}

	first := load("")
	if len(first.Items) != 20 || first.Limit != 20 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page=%+v", first)
	}
	if _, err = pool.Exec(ctx, `insert into content_ratings values(
		1000001,'r01000001',77,1,5,'concurrent','published',$1::timestamptz,$1::timestamptz)`, base.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	second := load(first.NextCursor)
	if len(second.Items) != 20 || !second.HasMore || second.NextCursor == "" {
		t.Fatalf("second page=%+v", second)
	}
	seen := make(map[string]struct{}, len(first.Items))
	for _, item := range first.Items {
		seen[item.ID] = struct{}{}
	}
	for _, item := range second.Items {
		if item.ID == "r01000001" {
			t.Fatal("newer concurrent rating drifted into an anchored later page")
		}
		if _, duplicate := seen[item.ID]; duplicate {
			t.Fatalf("duplicate rating across pages: %s", item.ID)
		}
	}

	page, err := parseRatingReviewPageRequest(url.Values{"limit": {"100"}}, rateableTarget{
		RouteID: 77, InternalID: 9, Type: "mod", PublicID: "perf05001",
	})
	if err != nil {
		t.Fatal(err)
	}
	page.Cursor = &ratingReviewPageCursor{
		Version:   ratingReviewPageCursorVersion,
		Scope:     page.Scope,
		UpdatedAt: base.Add(500000 * time.Microsecond),
		ID:        500000,
	}
	query, arguments := ratingReviewPageSQL(page)
	plan, wall := adminProjectScalePlan(t, ctx, pool, query, arguments...)
	if !strings.Contains(plan, "idx_content_ratings_target") || strings.Contains(plan, "seq scan on content_ratings") ||
		strings.Contains(plan, "sort  (") || wall > 2*time.Second {
		t.Fatalf("PERF050 deep rating page lost bounded index access in %s:\n%s", wall, plan)
	}
	t.Logf("PERF050 million-row handler uses five queries; deepPlanWall=%s", wall)
}
