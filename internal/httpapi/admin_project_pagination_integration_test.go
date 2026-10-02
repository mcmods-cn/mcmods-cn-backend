package httpapi

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestAdminProjectPaginationAndSearchStayIndexedAtScaleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate million-project administration plans")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	if _, err = pool.Exec(ctx, `create temporary table admin_project_catalog(
		object_route_id bigint primary key,public_id text not null unique,entity_type text not null,
		name text not null,canonical_path text not null,review_status text not null,
		view_count bigint not null,edit_count bigint not null,heat_score numeric(16,6) not null,
		rating_average numeric(6,4) not null,favorite_count bigint not null,comment_count bigint not null,
		download_count bigint not null,created_at timestamptz not null,updated_at timestamptz not null,
		last_edited_at timestamptz,search_document tsvector not null
	);
	create index idx_admin_project_catalog_heat
		on admin_project_catalog(heat_score desc,updated_at desc,object_route_id desc);
	create index idx_admin_project_catalog_type_heat
		on admin_project_catalog(entity_type,heat_score desc,updated_at desc,object_route_id desc);
	create index idx_admin_project_catalog_search on admin_project_catalog using gin(search_document)`); err != nil {
		t.Fatal(err)
	}

	var previousScale int64
	for _, scale := range []int64{100_000, 1_000_000} {
		started := time.Now()
		command, insertErr := pool.Exec(ctx, `insert into admin_project_catalog(
			object_route_id,public_id,entity_type,name,canonical_path,review_status,view_count,edit_count,
			heat_score,rating_average,favorite_count,comment_count,download_count,created_at,updated_at,
			last_edited_at,search_document
		) select value,'p'||lpad(value::text,8,'0'),case when value%2=0 then 'mod' else 'modpack' end,
			case when value%10000=0 then 'perf036needle project '||value else 'project '||value end,
			'/projects/'||value,'approved',value%10000,value%100,(value%1000)::numeric,4,1,2,3,
			timestamptz '2025-01-01 00:00:00+00',
			timestamptz '2025-01-01 00:00:00+00'+value*interval '1 microsecond',null,
			to_tsvector('simple','p'||lpad(value::text,8,'0')||' '||
				case when value%10000=0 then 'perf036needle project '||value else 'project '||value end)
		from generate_series($1::bigint+1,$2::bigint) value`, previousScale, scale)
		if insertErr != nil {
			t.Fatalf("populate PERF036 catalog at %d rows: %v", scale, insertErr)
		}
		if command.RowsAffected() != scale-previousScale {
			t.Fatalf("populate PERF036 catalog at %d rows affected=%d", scale, command.RowsAffected())
		}
		if _, err = pool.Exec(ctx, `analyze admin_project_catalog`); err != nil {
			t.Fatal(err)
		}

		first, parseErr := parseAdminProjectPageRequest(url.Values{"limit": {"40"}})
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		firstQuery, firstArguments := adminProjectPageSQL(first)
		firstPlan, firstPlanWall := adminProjectScalePlan(t, ctx, pool, firstQuery, firstArguments...)
		assertAdminProjectScaleIndex(t, scale, firstPlan, "idx_admin_project_catalog_heat")

		deep := first
		deep.Cursor = &adminProjectPageCursor{
			Heat: "500.000000", UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), RouteID: scale + 1,
		}
		deepQuery, deepArguments := adminProjectPageSQL(deep)
		deepPlan, deepPlanWall := adminProjectScalePlan(t, ctx, pool, deepQuery, deepArguments...)
		assertAdminProjectScaleIndex(t, scale, deepPlan, "idx_admin_project_catalog_heat")

		typed, parseErr := parseAdminProjectPageRequest(url.Values{"type": {"mod"}, "limit": {"40"}})
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		typedQuery, typedArguments := adminProjectPageSQL(typed)
		typedPlan, typedPlanWall := adminProjectScalePlan(t, ctx, pool, typedQuery, typedArguments...)
		assertAdminProjectScaleIndex(t, scale, typedPlan, "idx_admin_project_catalog_type_heat")

		search, parseErr := parseAdminProjectPageRequest(url.Values{"q": {"perf036needle"}, "limit": {"40"}})
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		searchQuery, searchArguments := adminProjectPageSQL(search)
		searchPlan, searchPlanWall := adminProjectScalePlan(t, ctx, pool, searchQuery, searchArguments...)
		assertAdminProjectSearchScalePlan(t, scale, searchPlan)
		for label, elapsed := range map[string]time.Duration{
			"first": firstPlanWall, "deep": deepPlanWall, "typed": typedPlanWall, "search": searchPlanWall,
		} {
			if elapsed > 2*time.Second {
				t.Fatalf("PERF036 %s query at %d rows exceeded the 2s regression budget: %s", label, scale, elapsed)
			}
		}

		rows, queryErr := pool.Query(ctx, deepQuery, deepArguments...)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		rowCount := 0
		for rows.Next() {
			rowCount++
		}
		rows.Close()
		if rows.Err() != nil || rowCount != 41 {
			t.Fatalf("PERF036 deep page at %d rows returned=%d err=%v", scale, rowCount, rows.Err())
		}
		t.Logf("PERF036 projects=%d first=%s deep=%s typed=%s search=%s loadAndPlans=%s",
			scale, firstPlanWall, deepPlanWall, typedPlanWall, searchPlanWall, time.Since(started))
		previousScale = scale
	}
}

func adminProjectScalePlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, arguments ...any) (string, time.Duration) {
	t.Helper()
	started := time.Now()
	rows, err := pool.Query(ctx, "explain(analyze,buffers,format text) "+query, arguments...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.ToLower(plan.String()), time.Since(started)
}

func assertAdminProjectScaleIndex(t *testing.T, scale int64, plan, index string) {
	t.Helper()
	if !strings.Contains(plan, strings.ToLower(index)) || strings.Contains(plan, "seq scan on admin_project_catalog") {
		t.Fatalf("PERF036 %d-row plan lost %s:\n%s", scale, index, plan)
	}
	if strings.Contains(plan, "offset") {
		t.Fatalf("PERF036 %d-row plan retained offset:\n%s", scale, plan)
	}
	if !strings.Contains(plan, "limit") {
		t.Fatalf("PERF036 %d-row plan is not bounded:\n%s", scale, plan)
	}
}

func assertAdminProjectSearchScalePlan(t *testing.T, scale int64, plan string) {
	t.Helper()
	usedSearchIndex := strings.Contains(plan, "idx_admin_project_catalog_search")
	usedBoundedSortIndex := strings.Contains(plan, "idx_admin_project_catalog_heat")
	if (!usedSearchIndex && !usedBoundedSortIndex) || strings.Contains(plan, "seq scan on admin_project_catalog") {
		t.Fatalf("PERF036 %d-row search lost both indexed access paths:\n%s", scale, plan)
	}
	if strings.Contains(plan, "offset") || !strings.Contains(plan, "limit") {
		t.Fatalf("PERF036 %d-row search is not a bounded keyset query:\n%s", scale, plan)
	}
}
