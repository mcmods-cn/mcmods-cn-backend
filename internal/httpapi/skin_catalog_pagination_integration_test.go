package httpapi

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSkinCatalogSearchAndDeepPagesStayIndexedAtMillionScaleIntegration(t *testing.T) {
	pool, ctx := openDeadLetterPageTestDB(t, 2*time.Minute)
	if _, err := pool.Exec(ctx, `create temporary table skin_public_catalog(
		asset_id bigint primary key,public_id text not null unique,kind text not null,model text not null,
		display_name text not null,created_at timestamptz not null,updated_at timestamptz not null,
		downloads bigint not null,heat_score numeric(16,6) not null,favorite_count bigint not null,
		bayesian_rating numeric(6,4) not null,rating_count bigint not null,view_count bigint not null,
		comment_count bigint not null,search_document tsvector not null)`); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	started := time.Now()
	if _, err := pool.Exec(ctx, `insert into skin_public_catalog(
		asset_id,public_id,kind,model,display_name,created_at,updated_at,downloads,heat_score,
		favorite_count,bayesian_rating,rating_count,view_count,comment_count,search_document)
		select value,'s'||lpad(value::text,8,'0'),case when value%2=0 then 'skin' else 'cape' end,
			case when value%3=0 then 'slim' else 'default' end,
			case when value%100000=0 then 'perf048needle texture '||value else 'texture '||value end,
			$1::timestamptz+value*interval '1 microsecond',$1::timestamptz+value*interval '2 microseconds',
			value%10000,(value%1000)::numeric,value%700,((value%500)::numeric/100),value%400,
			value%20000,value%300,
			to_tsvector('simple',case when value%100000=0 then 'perf048needle texture' else 'texture' end)
		from generate_series(1,1000000) value`, base); err != nil {
		t.Fatal(err)
	}
	// This is a read-plan fixture, not an ingestion benchmark. Bulk-build the
	// identical indexes after loading instead of maintaining ten indexes per
	// synthetic row. Keep the million-row cardinality and both budgets intact.
	if _, err := pool.Exec(ctx, `
	create index idx_skin_public_catalog_published on skin_public_catalog(created_at desc,updated_at desc,asset_id desc);
	create index idx_skin_public_catalog_updated on skin_public_catalog(updated_at desc,asset_id desc);
	create index idx_skin_public_catalog_heat on skin_public_catalog(heat_score desc,updated_at desc,asset_id desc);
	create index idx_skin_public_catalog_downloads on skin_public_catalog(downloads desc,updated_at desc,asset_id desc);
	create index idx_skin_public_catalog_favorites on skin_public_catalog(favorite_count desc,updated_at desc,asset_id desc);
	create index idx_skin_public_catalog_rating on skin_public_catalog(bayesian_rating desc,rating_count desc,updated_at desc,asset_id desc);
	create index idx_skin_public_catalog_views on skin_public_catalog(view_count desc,updated_at desc,asset_id desc);
	create index idx_skin_public_catalog_comments on skin_public_catalog(comment_count desc,updated_at desc,asset_id desc);
	create index idx_skin_public_catalog_name on skin_public_catalog(lower(display_name),asset_id);
	create index idx_skin_public_catalog_search on skin_public_catalog using gin(search_document);
	analyze skin_public_catalog`); err != nil {
		t.Fatal(err)
	}
	var cardinality int64
	if err := pool.QueryRow(ctx, `select count(*) from skin_public_catalog`).Scan(&cardinality); err != nil || cardinality != 1_000_000 {
		t.Fatalf("million-row fixture cardinality=%d err=%v", cardinality, err)
	}

	cases := []struct {
		name    string
		indexes []string
		page    skinCatalogPageRequest
	}{
		{name: "published deep", indexes: []string{"idx_skin_public_catalog_published"}, page: skinScaleRequest(t, url.Values{"sort": {"published"}, "limit": {"100"}}, &skinCatalogPageCursor{
			CreatedAt: base.Add(500000 * time.Microsecond), UpdatedAt: base.Add(1000000 * time.Microsecond), ID: 500000,
		})},
		{name: "views deep", indexes: []string{"idx_skin_public_catalog_views"}, page: skinScaleRequest(t, url.Values{"sort": {"views"}, "limit": {"100"}}, &skinCatalogPageCursor{
			Metric: "10000", UpdatedAt: base.Add(1000000 * time.Microsecond), ID: 500000,
		})},
		{name: "filtered published", indexes: []string{"idx_skin_public_catalog_published"}, page: skinScaleRequest(t, url.Values{"kind": {"cape"}, "model": {"default"}, "sort": {"published"}, "limit": {"100"}}, nil)},
		// PostgreSQL may choose either the GIN candidate path or scan the narrow
		// published keyset index while applying the indexed tsvector predicate.
		// Both avoid the old wide-table sequential scan and full result sort.
		{name: "search", indexes: []string{"idx_skin_public_catalog_search", "idx_skin_public_catalog_published"}, page: skinScaleRequest(t, url.Values{"q": {"perf048needle"}, "sort": {"published"}, "limit": {"100"}}, nil)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			query, arguments := skinCatalogPageSQL(testCase.page)
			plan, wall := adminProjectScalePlan(t, ctx, pool, query, arguments...)
			usedExpectedIndex := false
			for _, index := range testCase.indexes {
				usedExpectedIndex = usedExpectedIndex || strings.Contains(plan, index)
			}
			if !usedExpectedIndex || strings.Contains(plan, "seq scan on skin_public_catalog") || !strings.Contains(plan, "limit") {
				t.Fatalf("PERF048 plan lost bounded %v access:\n%s", testCase.indexes, plan)
			}
			if wall > 2*time.Second {
				t.Fatalf("PERF048 %s plan exceeded 2s: %s", testCase.name, wall)
			}
			rows := readSkinCatalogScaleRows(t, ctx, pool, query, arguments...)
			if len(rows) == 0 || len(rows) > testCase.page.Limit+1 {
				t.Fatalf("PERF048 %s rows=%d", testCase.name, len(rows))
			}
			t.Logf("PERF048 %s rows=%d planWall=%s", testCase.name, len(rows), wall)
		})
	}

	page := skinScaleRequest(t, url.Values{"sort": {"published"}, "limit": {"37"}}, nil)
	query, arguments := skinCatalogPageSQL(page)
	first := readSkinCatalogScaleRows(t, ctx, pool, query, arguments...)
	if len(first) != 38 {
		t.Fatalf("first page rows=%d want=38", len(first))
	}
	first = first[:37]
	page.Cursor = &skinCatalogPageCursor{
		Sort: page.Sort, Direction: page.Direction, CreatedAt: first[36].CreatedAt,
		UpdatedAt: first[36].UpdatedAt, ID: first[36].AssetID,
	}
	if _, err := pool.Exec(ctx, `insert into skin_public_catalog(
		asset_id,public_id,kind,model,display_name,created_at,updated_at,downloads,heat_score,
		favorite_count,bayesian_rating,rating_count,view_count,comment_count,search_document)
		values(1000001,'s01000001','skin','default','newer texture',$1,$1,0,0,0,0,0,0,0,to_tsvector('simple','newer'))`, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	query, arguments = skinCatalogPageSQL(page)
	second := readSkinCatalogScaleRows(t, ctx, pool, query, arguments...)
	seen := make(map[int64]struct{}, len(first))
	for _, row := range first {
		seen[row.AssetID] = struct{}{}
	}
	for _, row := range second {
		if _, duplicate := seen[row.AssetID]; duplicate || row.AssetID == 1000001 {
			t.Fatalf("cursor page drifted duplicate/new row %d", row.AssetID)
		}
	}
	t.Logf("PERF048 million projection loadAndPlans=%s", time.Since(started))
}

func skinScaleRequest(t *testing.T, values url.Values, cursor *skinCatalogPageCursor) skinCatalogPageRequest {
	t.Helper()
	page, err := parseSkinCatalogPageRequest(values)
	if err != nil {
		t.Fatal(err)
	}
	if cursor != nil {
		cursor.Version = skinCatalogPageCursorVersion
		cursor.Scope = page.Scope
		cursor.Sort = page.Sort
		cursor.Direction = page.Direction
		page.Cursor = cursor
	}
	return page
}

func readSkinCatalogScaleRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, arguments ...any) []skinCatalogPageRow {
	t.Helper()
	rows, err := pool.Query(ctx, query, arguments...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := make([]skinCatalogPageRow, 0)
	for rows.Next() {
		var row skinCatalogPageRow
		if err = rows.Scan(&row.AssetID, &row.SortName, &row.CreatedAt, &row.UpdatedAt,
			&row.Heat, &row.Downloads, &row.Favorites, &row.Rating, &row.RatingCount,
			&row.Views, &row.Comments); err != nil {
			t.Fatal(err)
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}
