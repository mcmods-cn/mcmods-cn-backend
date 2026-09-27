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

func TestCommunityPostCatalogBoundsMillionRowsAndOmitsBodiesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify community catalog pagination at scale")
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

	if _, err = pool.Exec(ctx, `create temporary table community_post_catalog(
		id bigint primary key,public_id text not null unique,kind text not null,category text not null,
		author_id bigint not null,title text not null,source_locale text not null,body_summary text not null,
		minecraft_versions text[] not null,mod_version_min text not null,mod_version_max text not null,
		severity text not null,has_fix boolean not null,issue_url text not null,cover_file_id bigint,
		resolution_status text not null,accepted_comment_id bigint,resolved_at timestamptz,
		review_status text not null,created_at timestamptz not null,updated_at timestamptz not null,
		published_at timestamptz not null,heat_score numeric not null,download_count bigint not null,
		favorite_count bigint not null,bayesian_rating numeric not null,rating_count bigint not null,
		view_count bigint not null,comment_count bigint not null,search_document tsvector not null);
	create index idx_community_post_catalog_published on community_post_catalog(
		kind,review_status,published_at desc,updated_at desc,id desc);
	create index idx_community_post_catalog_search on community_post_catalog using gin(search_document);
	create temporary table users(id bigint primary key,public_id text not null,username text not null);
	create temporary table oss_files(id bigint primary key,public_id text not null,object_key text not null,status text not null);
	create temporary table comments(id bigint primary key,public_id text not null);
	create temporary table community_post_project_refs(
		id bigint primary key,post_id bigint not null,target_id bigint,target_type text not null,
		raw_identifier text not null,display_order integer not null);
	create temporary table public_routes(
		id bigint primary key,public_id text not null,entity_type text not null,internal_id bigint not null,
		canonical_path text not null);
	create temporary table mods(id bigint primary key,review_status text not null,submitted_by bigint not null);
	create temporary table modpacks(id bigint primary key,review_status text not null,submitted_by bigint not null);
	create temporary table simple_projects(
		id bigint primary key,project_type text not null,review_status text not null,submitted_by bigint not null);
	create temporary table minecraft_servers(id bigint primary key,review_status text not null,submitted_by bigint not null);
	create temporary table community_posts(
		id bigint primary key,status text not null,review_status text not null,author_id bigint not null,
		body_markdown text not null);
	create temporary table blueprints(
		id bigint primary key,status text not null,review_status text not null,owner_id bigint not null);
	create temporary table skin_assets(
		id bigint primary key,status text not null,visibility text not null,review_status text not null,owner_id bigint not null);
	create temporary table community_post_resource_refs(
		id bigint primary key,post_id bigint not null,resource_id bigint,kind_code text not null,
		raw_resource_id text not null,display_order integer not null);
	create temporary table catalog_entities(id bigint primary key,public_id text not null);
	create temporary table game_resources(entity_id bigint primary key,canonical_id text not null);
	create temporary table mod_resource_version_details(
		resource_id bigint not null,status text not null,updated_at timestamptz not null,
		version_id bigint,icon_small_file_id bigint,icon_file_id bigint);
	create temporary table mod_content_versions(id bigint primary key,public_id text not null);
	create temporary table resource_import_snapshots(
		resource_id bigint not null,revision_id text not null,icon_path text not null,names jsonb not null,
		created_at timestamptz not null);
	create temporary table catalog_import_revisions(id text primary key,target_version_id bigint);
	create temporary table content_localizations(catalog_entity_id bigint not null,locale text not null,name text not null);
	create temporary table community_post_bounties(
		post_id bigint primary key,currency_id bigint not null,amount bigint not null,status text not null,
		tax_amount bigint not null,net_amount bigint not null);
	create temporary table currencies(
		id bigint primary key,code text not null,name text not null,icon text not null,translations jsonb not null);
	create temporary table system_settings(key text primary key,value jsonb not null)`); err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err = pool.Exec(ctx, `insert into users values(1,'u00000001','Scale author')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into community_post_catalog(
		id,public_id,kind,category,author_id,title,source_locale,body_summary,minecraft_versions,
		mod_version_min,mod_version_max,severity,has_fix,issue_url,resolution_status,review_status,
		created_at,updated_at,published_at,heat_score,download_count,favorite_count,bayesian_rating,
		rating_count,view_count,comment_count,search_document)
	select value,'p'||lpad(value::text,8,'0'),'tutorial','general',1,
		case when value%10000=0 then 'perf052needle post '||value else 'community post '||value end,
		'en-US',repeat('summary ',40),array['1.21.1'],'','','',false,'','open','approved',$1::timestamptz,
		$1::timestamptz+value*interval '1 microsecond',$1::timestamptz+value*interval '1 microsecond',
		(value%1000)::numeric,value,value%100,4.5,10,value%500,value%20,
		to_tsvector('simple',case when value%10000=0 then 'perf052needle post '||value else 'community post '||value end)
	from generate_series(1,1000000) value`, base); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into community_posts
	select value,'active','approved',1,'PERF052-SECRET-FULL-BODY-'||repeat('x',1048576)
	from generate_series(999977,1000000) value;
	analyze community_post_catalog; analyze users`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	loadPage := func(cursor, query string) communityPostPageResponse {
		t.Helper()
		path := "/api/v1/community/posts?kind=tutorial&limit=24"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		if query != "" {
			path += "&q=" + url.QueryEscape(query)
		}
		counter.queries.Store(0)
		request := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		response := httptest.NewRecorder()
		server.communityPosts(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("community page status=%d body=%s", response.Code, response.Body.String())
		}
		if queries := counter.queries.Load(); queries != 5 {
			t.Fatalf("community page queries=%d want=5 (page,settings,bounties,project refs,resource refs)", queries)
		}
		if response.Body.Len() > 64<<10 || strings.Contains(response.Body.String(), "bodyMarkdown") ||
			strings.Contains(response.Body.String(), "PERF052-SECRET-FULL-BODY") {
			t.Fatalf("community summary response leaked a full body or exceeded its bound: %d bytes", response.Body.Len())
		}
		var envelope struct {
			Data communityPostPageResponse `json:"data"`
		}
		if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}

	first := loadPage("", "")
	if len(first.Items) != 24 || !first.HasMore || first.NextCursor == "" || first.Items[0].ID != "p01000000" ||
		len(first.Items[0].Summary) > 320 || first.Items[0].Summary == "" {
		t.Fatalf("first community page items=%d more=%v cursor=%q first=%q summary=%d",
			len(first.Items), first.HasMore, first.NextCursor, first.Items[0].ID, len(first.Items[0].Summary))
	}
	if _, err = pool.Exec(ctx, `insert into community_post_catalog(
		id,public_id,kind,category,author_id,title,source_locale,body_summary,minecraft_versions,
		mod_version_min,mod_version_max,severity,has_fix,issue_url,resolution_status,review_status,
		created_at,updated_at,published_at,heat_score,download_count,favorite_count,bayesian_rating,
		rating_count,view_count,comment_count,search_document)
	values(1000001,'p01000001','tutorial','general',1,'concurrent post','en-US','concurrent summary',
		array['1.21.1'],'','','',false,'','open','approved',$1,$1,$1,0,0,0,0,0,0,0,to_tsvector('simple','concurrent post'))`,
		base.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	second := loadPage(first.NextCursor, "")
	seen := make(map[string]struct{}, len(first.Items))
	for _, item := range first.Items {
		seen[item.ID] = struct{}{}
	}
	for _, item := range second.Items {
		if item.ID == "p01000001" {
			t.Fatal("concurrently inserted newer post drifted into an anchored later page")
		}
		if _, duplicate := seen[item.ID]; duplicate {
			t.Fatalf("duplicate community post across pages: %s", item.ID)
		}
	}
	search := loadPage("", "perf052needle")
	if len(search.Items) != 24 || !search.HasMore || search.NextCursor == "" {
		t.Fatalf("search page items=%d more=%v cursor=%q", len(search.Items), search.HasMore, search.NextCursor)
	}

	deep, err := parseCommunityPostPageRequest(url.Values{
		"kind": {"tutorial"}, "limit": {"100"}, "sort": {"published"}, "order": {"desc"},
	}, security.Claims{})
	if err != nil {
		t.Fatal(err)
	}
	deep.Cursor = &communityPostPageCursor{
		Version: communityPostPageCursorVersion, Scope: deep.Scope, Sort: deep.Sort, Direction: deep.Direction,
		ID: 500001, PublishedAt: base.Add(500001 * time.Microsecond), UpdatedAt: base.Add(500001 * time.Microsecond),
	}
	deepQuery, deepArguments := communityPostPageSQL(deep)
	deepPlan, deepWall := adminProjectScalePlan(t, ctx, pool, deepQuery, deepArguments...)
	if !strings.Contains(deepPlan, "idx_community_post_catalog_published") ||
		strings.Contains(deepPlan, "seq scan on community_post_catalog") || deepWall > 2*time.Second {
		t.Fatalf("PERF052 deep page lost bounded published index access in %s:\n%s", deepWall, deepPlan)
	}
	searchRequest, err := parseCommunityPostPageRequest(url.Values{
		"kind": {"tutorial"}, "q": {"perf052needle"}, "limit": {"100"},
		"sort": {"relevance"}, "order": {"desc"},
	}, security.Claims{})
	if err != nil {
		t.Fatal(err)
	}
	searchQuery, searchArguments := communityPostPageSQL(searchRequest)
	searchPlan, searchWall := adminProjectScalePlan(t, ctx, pool, searchQuery, searchArguments...)
	if !strings.Contains(searchPlan, "idx_community_post_catalog_search") ||
		strings.Contains(searchPlan, "seq scan on community_post_catalog") || searchWall > 2*time.Second {
		t.Fatalf("PERF052 search lost GIN access in %s:\n%s", searchWall, searchPlan)
	}
	t.Logf("PERF052 million-row summary handler=5 queries; deep/search plans=%s/%s", deepWall, searchWall)
}
