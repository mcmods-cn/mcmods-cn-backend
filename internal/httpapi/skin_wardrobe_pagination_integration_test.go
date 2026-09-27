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
	"mcmods-cn-backend/internal/security"
)

type skinWardrobeHandlerPage struct {
	Items []struct {
		PublicID string `json:"publicId"`
	} `json:"items"`
	Total      int64  `json:"total"`
	Limit      int    `json:"limit"`
	HasMore    bool   `json:"hasMore"`
	NextCursor string `json:"nextCursor"`
}

func TestSkinWardrobeHandlerBoundsFiveThousandItemsWithStableCursorIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify wardrobe pagination")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	if _, err = pool.Exec(ctx, `create temporary table users(
		id bigint primary key,public_id text not null,username text not null);
	create temporary table skin_texture_blobs(
		hash text primary key,width integer not null,height integer not null,size_bytes bigint not null);
	create temporary table skin_assets(
		id bigint primary key,public_id text not null,owner_id bigint not null,blob_hash text not null,
		kind text not null,model text not null,display_name text not null,description text not null,tags text[] not null,
		visibility text not null,review_status text not null,status text not null,downloads bigint not null,
		created_at timestamptz not null,updated_at timestamptz not null);
	create temporary table skin_wardrobe(
		user_id bigint not null,asset_id bigint not null,added_at timestamptz not null,primary key(user_id,asset_id));
	create index idx_skin_wardrobe_user_added on skin_wardrobe(user_id,added_at desc,asset_id)`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into users values(1,'owner049','Owner')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into skin_texture_blobs
	select md5(value::text)||md5('blob'||value::text),64,64,2048 from generate_series(1,5001) value;
	`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into skin_assets
	select value,'w'||lpad(value::text,8,'0'),1,md5(value::text)||md5('blob'||value::text),
		case when value%2=0 then 'skin' else 'cape' end,
		case when value%3=0 then 'slim' else 'default' end,
		'Wardrobe texture '||value,repeat('description ',8),array['wardrobe','perf049'],
		'private','approved','active',value,$1::timestamptz,$1::timestamptz
	from generate_series(1,5001) value`, base); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into skin_wardrobe
	select 1,value,$1::timestamptz+value*interval '1 microsecond' from generate_series(1,5000) value`, base); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `analyze skin_wardrobe; analyze skin_assets`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	load := func(path string) (skinWardrobeHandlerPage, int) {
		t.Helper()
		counter.queries.Store(0)
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 1}))
		response := httptest.NewRecorder()
		server.skinWardrobe(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		if queries := counter.queries.Load(); queries != 2 {
			t.Fatalf("wardrobe page queries=%d want=2", queries)
		}
		var envelope struct {
			Data skinWardrobeHandlerPage `json:"data"`
		}
		if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data, response.Body.Len()
	}
	first, firstBytes := load("/api/v1/users/me/skin-wardrobe?limit=100")
	if len(first.Items) != 100 || first.Total != 5000 || !first.HasMore || first.NextCursor == "" || firstBytes > 512<<10 {
		t.Fatalf("first items=%d total=%d more=%t cursor=%q bytes=%d", len(first.Items), first.Total, first.HasMore, first.NextCursor, firstBytes)
	}
	if _, err = pool.Exec(ctx, `insert into skin_wardrobe values(1,5001,$1::timestamptz+interval '1 second')`, base); err != nil {
		t.Fatal(err)
	}
	second, secondBytes := load("/api/v1/users/me/skin-wardrobe?limit=100&cursor=" + url.QueryEscape(first.NextCursor))
	if len(second.Items) != 100 || !second.HasMore || second.NextCursor == "" || secondBytes > 512<<10 {
		t.Fatalf("second items=%d more=%t cursor=%q bytes=%d", len(second.Items), second.HasMore, second.NextCursor, secondBytes)
	}
	seen := make(map[string]struct{}, len(first.Items))
	for _, item := range first.Items {
		seen[item.PublicID] = struct{}{}
	}
	for _, item := range second.Items {
		if item.PublicID == "w00005001" {
			t.Fatal("newer concurrent wardrobe item drifted into an anchored later page")
		}
		if _, duplicate := seen[item.PublicID]; duplicate {
			t.Fatalf("duplicate across wardrobe pages: %s", item.PublicID)
		}
	}
	filtered, _ := load("/api/v1/users/me/skin-wardrobe?kind=skin&model=slim&limit=100")
	if len(filtered.Items) != 100 || filtered.Total != 833 || !filtered.HasMore {
		t.Fatalf("filtered items=%d total=%d more=%t", len(filtered.Items), filtered.Total, filtered.HasMore)
	}

	request, err := parseSkinWardrobePageRequest(url.Values{"limit": {"100"}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor = &skinWardrobePageCursor{
		Version: skinWardrobePageCursorVersion, Scope: request.Scope,
		AddedAt: base.Add(2500 * time.Microsecond), AssetID: 2500,
	}
	query, arguments := skinWardrobePageSQL(request)
	plan, wall := adminProjectScalePlan(t, ctx, pool, query, arguments...)
	if !strings.Contains(plan, "idx_skin_wardrobe_user_added") || strings.Contains(plan, "seq scan on skin_wardrobe") ||
		strings.Contains(plan, "sort  (") || wall > 2*time.Second {
		t.Fatalf("PERF049 deep wardrobe page lost bounded index access in %s:\n%s", wall, plan)
	}
	t.Logf("PERF049 firstBytes=%d secondBytes=%d deepPlanWall=%s", firstBytes, secondBytes, wall)
}
