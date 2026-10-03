package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestBlueprintCatalogKeysetCursorTraversesEverySortBeyondSixtyIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to execute blueprint pagination against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
	if _, err = pool.Exec(ctx, `
		create temporary table users (
			id bigint primary key, username text not null, avatar_url text not null default ''
		);
		create temporary table blueprints (
			id bigint primary key, public_id text not null, title text not null,
			description_markdown text not null default '', source_format text not null default 'nbt',
			status text not null default 'ready', review_status text not null default 'approved',
			size_x integer not null default 1, size_y integer not null default 1, size_z integer not null default 1,
			block_count bigint not null default 1, palette_count integer not null default 1,
			owner_id bigint not null, created_at timestamptz not null, updated_at timestamptz not null
		);
		create temporary table blueprint_mods (blueprint_id bigint, mod_id bigint, source_namespace text);
		create temporary table mods (
			id bigint primary key, project_code text, slug text, primary_name text,
			secondary_name text, abbreviation text, icon_url text, review_status text not null default 'approved'
		);
		create temporary table mod_identifiers (
			id bigint primary key, mod_id bigint, identifier text, is_primary boolean, display_order integer
		);
		create temporary table public_routes (id bigint primary key, entity_type text, internal_id bigint);
		create temporary table content_popularity_stats (
			object_route_id bigint primary key, heat_score numeric(16,6), download_count bigint,
			favorite_count bigint, bayesian_rating numeric(6,4), rating_count bigint,
			view_count bigint, comment_count bigint
		);
		insert into users values(1,'blueprint-owner','');
		insert into blueprints(id,public_id,title,description_markdown,owner_id,created_at,updated_at)
		select value,'b'||lpad(value::text,8,'0'),
			case when value%2=0 then 'Castle ' else 'Tower ' end||lpad(value::text,3,'0'),
			case when value%2=0 then 'red castle' else 'blue tower' end,1,
			timestamptz '2026-08-21 00:00:00+00'+((value-1)/3)*interval '1 second',
			timestamptz '2026-08-21 01:00:00+00'+((value-1)/5)*interval '1 second'
		from generate_series(1,125) value;
		insert into public_routes select 1000+id,'blueprint',id from blueprints;
		insert into content_popularity_stats
		select 1000+id,(id%11)::numeric,(id%17),(id%13),((id%50)/10.0)::numeric(6,4),id%9,id%19,id%7
		from blueprints`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}

	for _, sort := range []string{
		"name", "published", "updated", "collected", "heat", "downloads",
		"favorites", "rating", "views", "comments", "relevance",
	} {
		for _, direction := range []string{"asc", "desc"} {
			t.Run(sort+"_"+direction, func(t *testing.T) {
				seen := map[string]struct{}{}
				cursor := ""
				for pageNumber := 0; ; pageNumber++ {
					path := fmt.Sprintf("/api/v1/blueprints?limit=36&sort=%s&order=%s", sort, direction)
					if cursor != "" {
						path += "&cursor=" + url.QueryEscape(cursor)
					}
					page := loadBlueprintIntegrationPage(t, server, path)
					for _, item := range page.Items {
						if _, duplicate := seen[item.ID]; duplicate {
							t.Fatalf("page %d repeated %s", pageNumber, item.ID)
						}
						seen[item.ID] = struct{}{}
					}
					if !page.HasMore {
						if page.NextCursor != "" {
							t.Fatal("terminal page returned a cursor")
						}
						break
					}
					if page.NextCursor == "" {
						t.Fatal("non-terminal page omitted its cursor")
					}
					cursor = page.NextCursor
				}
				if len(seen) != 125 {
					t.Fatalf("traversed %d of 125 blueprints", len(seen))
				}
			})
		}
	}

	first := loadBlueprintIntegrationPage(t, server, "/api/v1/blueprints?limit=10&q=castle&sort=name&order=asc")
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/blueprints?limit=10&q=tower&sort=name&order=asc&cursor="+url.QueryEscape(first.NextCursor), nil)
	response := httptest.NewRecorder()
	server.blueprints(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("cross-filter cursor returned %d: %s", response.Code, response.Body.String())
	}
}

type blueprintIntegrationPage struct {
	Items []struct {
		ID string `json:"id"`
	} `json:"items"`
	HasMore    bool   `json:"hasMore"`
	NextCursor string `json:"nextCursor"`
}

func loadBlueprintIntegrationPage(t *testing.T, server *Server, path string) blueprintIntegrationPage {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	server.blueprints(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("%s returned %d: %s", path, response.Code, response.Body.String())
	}
	var envelope struct {
		Data blueprintIntegrationPage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}
