package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestProjectChangelogSummaryPagesStayBoundedAtOneMillionRowsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate changelog summary keyset scale")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	poolConfig.ConnConfig.Tracer = counter
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	fixtureStarted := time.Now()
	if _, err = pool.Exec(ctx, `
		create temporary table users (
			id bigint primary key,public_id text not null,username text not null
		);
		create temporary table project_changelog_categories (
			id bigint primary key,public_id text not null,object_route_id bigint not null,default_locale text not null
		);
		create temporary table project_changelog_category_localizations (
			category_id bigint not null,locale text not null,name text not null,primary key(category_id,locale)
		);
		create temporary table project_changelogs (
			id bigint primary key,public_id text not null,object_route_id bigint not null,category_id bigint,
			event_at timestamptz not null,minecraft_versions text[] not null,project_version text not null,
			default_locale text not null,status text not null,review_status text not null,created_by bigint not null,
			created_at timestamptz not null,updated_at timestamptz not null
		);
		create index idx_project_changelogs_target
			on project_changelogs(object_route_id,review_status,event_at desc,id desc) where status='active';
		create temporary table project_changelog_localizations (
			changelog_id bigint not null,locale text not null,body_markdown text not null,primary key(changelog_id,locale)
		);
		create temporary table change_requests (
			aggregate_type text not null,aggregate_key text not null,status text not null
		);
		create index idx_change_requests_pending on change_requests(aggregate_type,aggregate_key,status);
		insert into users values(1,'u00000001','Scale author');
		insert into project_changelogs
		select value,'c'||lpad(value::text,8,'0'),42,null,
			timestamptz '2026-01-01 00:00:00+00'+value*interval '1 second',array['1.21.1'],'v'||value,
			'zh-CN','active','approved',1,timestamptz '2026-01-01 00:00:00+00',timestamptz '2026-01-01 00:00:00+00'
		from generate_series(1,1000000) value;
		insert into project_changelog_localizations
		select value,'zh-CN','summary-'||value from generate_series(1,1000000) value;
		update project_changelog_localizations set body_markdown=repeat('正文',100000)||'SECRET-TAIL'
		where changelog_id=1000000;
		insert into project_changelog_localizations
		select 1000000,locale,repeat(locale,25000)||'SECRET-TAIL'
		from unnest(array['en-US','ja-JP','ko-KR','ru-RU','fr-FR','de-DE','es-ES']) locale;
		analyze project_changelogs;
		analyze project_changelog_localizations;
		analyze change_requests
	`); err != nil {
		t.Fatal(err)
	}
	t.Logf("1m changelog fixture loaded in %s", time.Since(fixtureStarted))

	api := &Server{db: pool}
	target := projectChangelogTarget{RouteID: 42, EntityType: "mod", PublicID: "scale0001", CanEdit: true}
	cursor := ""
	seen := make(map[string]struct{}, 100)
	counter.queries.Store(0)
	for pageNumber := 0; pageNumber < 2; pageNumber++ {
		request, parseErr := parseProjectChangelogPageRequest(map[string][]string{
			"locale": {"zh-CN"}, "limit": {"50"}, "cursor": {cursor},
		}, target, "zh-CN")
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		page, pageErr := api.loadProjectChangelogs(ctx, target, "zh-CN", request)
		if pageErr != nil {
			t.Fatal(pageErr)
		}
		if len(page.Items) != 50 || !page.HasMore || page.NextCursor == "" {
			t.Fatalf("page %d items=%d hasMore=%t cursor=%q", pageNumber, len(page.Items), page.HasMore, page.NextCursor)
		}
		encoded, marshalErr := json.Marshal(page.Items)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if len(encoded) >= 256*1024 || strings.Contains(string(encoded), "SECRET-TAIL") || strings.Contains(string(encoded), "localizations") {
			t.Fatalf("summary page leaked full bodies or exceeded its byte bound: %d bytes", len(encoded))
		}
		for _, item := range page.Items {
			if _, duplicate := seen[item.ID]; duplicate {
				t.Fatalf("duplicate changelog %s", item.ID)
			}
			seen[item.ID] = struct{}{}
		}
		cursor = page.NextCursor
	}
	if queries := counter.queries.Load(); queries != 2 {
		t.Fatalf("two changelog pages executed %d SQL statements, want 2", queries)
	}

	for name, anchorID := range map[string]int64{"100k depth": 900001, "1m depth": 51} {
		t.Run(name, func(t *testing.T) {
			request := projectChangelogPageRequest{Limit: 50, Cursor: &projectChangelogPageCursor{
				EventAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(anchorID) * time.Second), ID: anchorID,
			}}
			query, args := projectChangelogPageSQL(42, "zh-CN", request)
			rows, explainErr := pool.Query(ctx, "explain (analyze,buffers,format text) "+query, args...)
			if explainErr != nil {
				t.Fatal(explainErr)
			}
			planLines := make([]string, 0)
			for rows.Next() {
				var line string
				if scanErr := rows.Scan(&line); scanErr != nil {
					rows.Close()
					t.Fatal(scanErr)
				}
				planLines = append(planLines, line)
			}
			if rowsErr := rows.Err(); rowsErr != nil {
				rows.Close()
				t.Fatal(rowsErr)
			}
			rows.Close()
			plan := strings.Join(planLines, "\n")
			if !strings.Contains(plan, "idx_project_changelogs_target") || strings.Contains(plan, "Seq Scan on project_changelogs") {
				t.Fatalf("changelog page did not use its bounded keyset index:\n%s", plan)
			}
			t.Logf("%s changelog keyset plan:\n%s", name, plan)
		})
	}
}
