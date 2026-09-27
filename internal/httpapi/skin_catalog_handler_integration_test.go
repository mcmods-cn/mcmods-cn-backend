package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

type skinCatalogHandlerPage struct {
	Items []struct {
		PublicID string `json:"publicId"`
	} `json:"items"`
	Limit      int    `json:"limit"`
	HasMore    bool   `json:"hasMore"`
	NextCursor string `json:"nextCursor"`
}

func TestPublicSkinCatalogHandlerUsesTwoQueriesAndStableCursorIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the public skin catalog handler")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	if _, err = pool.Exec(ctx, `create temporary table users(
		id bigint primary key,public_id text not null,username text not null);
	create temporary table skin_texture_blobs(
		hash text primary key,width integer not null,height integer not null,size_bytes bigint not null);
	create temporary table skin_assets(
		id bigint primary key,public_id text not null,owner_id bigint not null,blob_hash text not null,
		kind text not null,model text not null,display_name text not null,description text not null,tags text[] not null,
		visibility text not null,review_status text not null,status text not null,downloads bigint not null,
		created_at timestamptz not null,updated_at timestamptz not null);
	create temporary table skin_wardrobe(user_id bigint not null,asset_id bigint not null,primary key(user_id,asset_id));
	create temporary table skin_public_catalog(
		asset_id bigint primary key,public_id text not null,kind text not null,model text not null,
		display_name text not null,created_at timestamptz not null,updated_at timestamptz not null,
		downloads bigint not null,heat_score numeric not null,favorite_count bigint not null,
		bayesian_rating numeric not null,rating_count bigint not null,view_count bigint not null,
		comment_count bigint not null,search_document tsvector not null);
	insert into users values(1,'owner048','Owner');
	insert into skin_texture_blobs
	select md5(value::text)||md5('x'||value::text),64,64,1024 from generate_series(1,75) value;
	insert into skin_assets
	select value,'s'||lpad(value::text,8,'0'),1,md5(value::text)||md5('x'||value::text),
		case when value%2=0 then 'skin' else 'cape' end,'default','Texture '||value,'Description',array['catalog'],
		'public','approved','active',value,now()-value*interval '1 second',now()-value*interval '1 second'
	from generate_series(1,75) value;
	insert into skin_public_catalog
	select id,public_id,kind,model,display_name,created_at,updated_at,downloads,0,0,0,0,0,0,
		to_tsvector('simple',display_name||' '||array_to_string(tags,' ')) from skin_assets`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	load := func(path string) skinCatalogHandlerPage {
		t.Helper()
		counter.queries.Store(0)
		request := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		response := httptest.NewRecorder()
		server.listPublicSkins(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		var envelope struct {
			Data skinCatalogHandlerPage `json:"data"`
		}
		if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if queries := counter.queries.Load(); queries != 2 {
			t.Fatalf("skin catalog handler queries=%d want=2", queries)
		}
		return envelope.Data
	}
	first := load("/api/v1/skins?limit=36&sort=published&order=desc")
	if len(first.Items) != 36 || !first.HasMore || first.NextCursor == "" || first.Limit != 36 {
		t.Fatalf("first page items=%d hasMore=%t cursor=%q limit=%d", len(first.Items), first.HasMore, first.NextCursor, first.Limit)
	}
	second := load("/api/v1/skins?limit=36&sort=published&order=desc&cursor=" + url.QueryEscape(first.NextCursor))
	if len(second.Items) != 36 || !second.HasMore || second.NextCursor == "" {
		t.Fatalf("second page items=%d hasMore=%t cursor=%q", len(second.Items), second.HasMore, second.NextCursor)
	}
	seen := make(map[string]struct{}, len(first.Items))
	for _, item := range first.Items {
		seen[item.PublicID] = struct{}{}
	}
	for _, item := range second.Items {
		if _, duplicate := seen[item.PublicID]; duplicate {
			t.Fatalf("duplicate public ID across cursor pages: %s", item.PublicID)
		}
	}
}
