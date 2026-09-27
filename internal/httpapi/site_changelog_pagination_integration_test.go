package httpapi

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestSiteChangelogPagesStayIndexedAndReachableAtOneMillionRowsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate million-row site changelog pages")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		databaseURL = config.Load().DB.ConnString()
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
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
	if err = pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	fixtureStarted := time.Now()
	if _, err = pool.Exec(ctx, `
		create temporary table site_changelogs (
			id bigint primary key,public_id text not null,change_date date not null,status text not null,
			updated_at timestamptz not null
		);
		create index idx_site_changelogs_public
			on site_changelogs(change_date desc,id desc) where status='published';
		create index idx_site_changelogs_admin on site_changelogs(change_date desc,id desc);
		create temporary table site_changelog_translations (
			changelog_id bigint not null,locale text not null,title text not null,body_markdown text not null,status text not null,
			primary key(changelog_id,locale)
		);
		create index idx_site_changelog_translations_published
			on site_changelog_translations(changelog_id,locale) where status='published';
		insert into site_changelogs
		select value,lpad(value::text,9,'0'),date '2000-01-01'+((value-1)/1000)::integer,
			case when value%2=0 then 'published' else 'draft' end,
			timestamptz '2026-01-01 00:00:00+00'+value*interval '1 second'
		from generate_series(1,1000000) value;
		insert into site_changelog_translations
		select value,'zh-CN','title-'||value,'body-'||value,'published' from generate_series(1,1000000) value;
		analyze site_changelogs;
		analyze site_changelog_translations
	`); err != nil {
		t.Fatal(err)
	}
	t.Logf("1m site changelogs and translations loaded in %s", time.Since(fixtureStarted))

	server := &Server{db: pool}
	publicRequest, err := parseSiteChangelogPageRequest(url.Values{"locale": {"zh-CN"}, "limit": {"50"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	before := counter.queries.Load()
	first, err := server.queryPublicSiteChangelogPage(ctx, publicRequest)
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-before != 1 {
		t.Fatalf("public page executed %d SQL statements, want 1", counter.queries.Load()-before)
	}
	if len(first.Items) != 50 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first public page items=%d hasMore=%t cursor=%t", len(first.Items), first.HasMore, first.NextCursor != "")
	}
	seen := make(map[string]struct{}, len(first.Items))
	for _, item := range first.Items {
		seen[item.ID] = struct{}{}
	}

	if _, err = pool.Exec(ctx, `
		insert into site_changelogs values(1000001,'001000001',date '2100-01-01','published',now());
		insert into site_changelog_translations values(1000001,'zh-CN','concurrent-head','concurrent-head','published')
	`); err != nil {
		t.Fatal(err)
	}
	secondRequest, err := parseSiteChangelogPageRequest(url.Values{
		"locale": {"zh-CN"}, "limit": {"50"}, "cursor": {first.NextCursor},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	before = counter.queries.Load()
	second, err := server.queryPublicSiteChangelogPage(ctx, secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-before != 1 {
		t.Fatalf("second public page executed %d SQL statements, want 1", counter.queries.Load()-before)
	}
	for _, item := range second.Items {
		if item.ID == "001000001" {
			t.Fatal("concurrent head insertion crossed an existing page cursor")
		}
		if _, duplicate := seen[item.ID]; duplicate {
			t.Fatalf("duplicate public changelog %s across cursor pages", item.ID)
		}
	}

	publicDeep := publicRequest
	publicDeep.Cursor = &siteChangelogPageCursor{
		Version: siteChangelogPageCursorVersion, Scope: publicDeep.Scope, Limit: publicDeep.Limit,
		ChangeDate: time.Date(2000, time.January, 2, 0, 0, 0, 0, time.UTC), ID: 1002,
	}
	before = counter.queries.Load()
	deepPublicPage, err := server.queryPublicSiteChangelogPage(ctx, publicDeep)
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-before != 1 || len(deepPublicPage.Items) != 50 {
		t.Fatalf("deep public page queries=%d items=%d", counter.queries.Load()-before, len(deepPublicPage.Items))
	}

	adminRequest, err := parseSiteChangelogPageRequest(url.Values{"limit": {"50"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	adminRequest.Cursor = &siteChangelogPageCursor{
		Version: siteChangelogPageCursorVersion, Scope: adminRequest.Scope, Limit: adminRequest.Limit,
		ChangeDate: time.Date(2000, time.January, 2, 0, 0, 0, 0, time.UTC), ID: 1001,
	}
	before = counter.queries.Load()
	deepAdminPage, err := server.queryAdminSiteChangelogPage(ctx, adminRequest)
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-before != 1 || len(deepAdminPage.Items) != 50 || deepAdminPage.NextCursor == "" {
		t.Fatalf("deep admin page queries=%d items=%d cursor=%t", counter.queries.Load()-before, len(deepAdminPage.Items), deepAdminPage.NextCursor != "")
	}

	publicDate, publicID := siteChangelogPagePosition(publicDeep)
	publicPlan := siteChangelogScalePlan(t, ctx, pool, siteChangelogPublicPageSQL,
		publicDeep.Locale, publicDate, publicID, publicDeep.Limit+1)
	if !strings.Contains(publicPlan, "idx_site_changelogs_public") || !strings.Contains(publicPlan, "idx_site_changelog_translations_published") ||
		strings.Contains(publicPlan, "Seq Scan on site_changelogs") || strings.Contains(publicPlan, "Seq Scan on site_changelog_translations") {
		t.Fatalf("million-row public page lost its keyset index:\n%s", publicPlan)
	}
	adminDate, adminID := siteChangelogPagePosition(adminRequest)
	adminPlan := siteChangelogScalePlan(t, ctx, pool, siteChangelogAdminPageSQL,
		adminDate, adminID, adminRequest.Limit+1)
	if !strings.Contains(adminPlan, "idx_site_changelogs_admin") || strings.Contains(adminPlan, "Seq Scan on site_changelogs") {
		t.Fatalf("million-row admin page lost its keyset index:\n%s", adminPlan)
	}
	t.Logf("public and admin million-row deep pages each used one SQL statement and their keyset indexes")
}

func siteChangelogScalePlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) string {
	t.Helper()
	rows, err := pool.Query(ctx, fmt.Sprintf("explain (analyze,buffers,format text) %s", query), args...)
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
	return plan.String()
}
