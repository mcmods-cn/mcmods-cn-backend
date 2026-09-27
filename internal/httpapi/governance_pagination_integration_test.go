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

func TestGovernancePagesStayIndexedAndStableAtOneMillionRowsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate million-row governance pages")
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
		create temporary table users (
			id bigint primary key,public_id text not null,username text not null
		);
		insert into users select value,lpad(value::text,9,'0'),'user-'||value from generate_series(1,100) value;
		create temporary table reports (
			id bigint primary key,public_id text not null,target_type text not null,target_public_id text not null,
			reporter_id bigint not null,reason_code text not null,status text not null,
			created_at timestamptz not null,resolved_at timestamptz,claimed_at timestamptz
		);
		create index idx_reports_reporter_history on reports(reporter_id,created_at desc,id desc);
		create index idx_reports_queue on reports(status,created_at,id);
		insert into reports
		select value,lpad(value::text,9,'0'),'mod','target-'||value,1,'spam','pending',
			timestamptz '2000-01-01 00:00:00+00'+value*interval '1 second',null,null
		from generate_series(1,1000000) value;
		create temporary table ban_records (
			id bigint primary key,public_id text not null,user_id bigint not null,username_snapshot text not null,
			avatar_snapshot text not null,reason_code text not null,custom_reason text not null,status text not null,
			starts_at timestamptz not null,ends_at timestamptz,revoked_at timestamptz,created_at timestamptz not null
		);
		create index idx_ban_records_public on ban_records(created_at desc,id desc);
		insert into ban_records
		select value,lpad(value::text,9,'0'),1,'user-1','','spam','','active',
			timestamptz '2000-01-01 00:00:00+00'+value*interval '1 second',null,null,
			timestamptz '2000-01-01 00:00:00+00'+value*interval '1 second'
		from generate_series(1,1000000) value;
		analyze users;
		analyze reports;
		analyze ban_records
	`); err != nil {
		t.Fatal(err)
	}
	t.Logf("1m reports and 1m ban records loaded in %s", time.Since(fixtureStarted))

	server := &Server{db: pool}
	testOwnReportPageStability(t, ctx, pool, server, counter)
	testAdminReportPageStability(t, ctx, pool, server, counter)
	testBlackroomPageStability(t, ctx, pool, server, counter)
	t.Log("all deep governance pages used one SQL statement and their tuple-keyset indexes")
}

func testOwnReportPageStability(t *testing.T, ctx context.Context, pool *pgxpool.Pool, server *Server, counter *integrationQueryCounter) {
	t.Helper()
	request, err := parseOwnReportPageRequest(url.Values{"limit": {"50"}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	before := counter.queries.Load()
	first, err := server.queryOwnReportPage(ctx, 1, request)
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-before != 1 || len(first.Items) != 50 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first own-report page queries=%d items=%d hasMore=%t cursor=%t", counter.queries.Load()-before, len(first.Items), first.HasMore, first.NextCursor != "")
	}
	seen := reportIDs(first.Items)
	if _, err = pool.Exec(ctx, `insert into reports values(1000001,'001000001','mod','concurrent-head',1,'spam','pending',timestamptz '2100-01-01',null,null)`); err != nil {
		t.Fatal(err)
	}
	secondRequest, err := parseOwnReportPageRequest(url.Values{"limit": {"50"}, "cursor": {first.NextCursor}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	before = counter.queries.Load()
	second, err := server.queryOwnReportPage(ctx, 1, secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-before != 1 {
		t.Fatalf("second own-report page executed %d SQL statements, want 1", counter.queries.Load()-before)
	}
	for _, item := range second.Items {
		if item.ID == "001000001" {
			t.Fatal("concurrent own-report head crossed an existing cursor")
		}
		if _, duplicate := seen[item.ID]; duplicate {
			t.Fatalf("duplicate own report %s across pages", item.ID)
		}
	}
	deep := request
	deep.Cursor = &governancePageCursor{Version: governancePageCursorVersion, Scope: deep.Scope,
		CreatedAt: time.Date(2000, 1, 7, 18, 53, 20, 0, time.UTC), ID: 500000}
	before = counter.queries.Load()
	page, err := server.queryOwnReportPage(ctx, 1, deep)
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-before != 1 || len(page.Items) != 50 {
		t.Fatalf("deep own-report page queries=%d items=%d", counter.queries.Load()-before, len(page.Items))
	}
	createdAt, id := governancePagePosition(deep)
	plan := governanceScalePlan(t, ctx, pool, ownReportPageSQL, int64(1), createdAt, id, deep.Limit+1)
	if !strings.Contains(plan, "idx_reports_reporter_history") || strings.Contains(plan, "Seq Scan on reports") {
		t.Fatalf("million-row own-report page lost its reporter index:\n%s", plan)
	}
}

func testAdminReportPageStability(t *testing.T, ctx context.Context, pool *pgxpool.Pool, server *Server, counter *integrationQueryCounter) {
	t.Helper()
	request, err := parseAdminReportPageRequest(url.Values{"status": {"pending"}, "limit": {"50"}})
	if err != nil {
		t.Fatal(err)
	}
	before := counter.queries.Load()
	first, err := server.queryAdminReportPage(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-before != 1 || len(first.Items) != 50 || first.NextCursor == "" {
		t.Fatalf("first admin-report page queries=%d items=%d cursor=%t", counter.queries.Load()-before, len(first.Items), first.NextCursor != "")
	}
	seen := adminReportIDs(first.Items)
	if _, err = pool.Exec(ctx, `insert into reports values(1000002,'001000002','mod','concurrent-head',1,'spam','pending',timestamptz '1990-01-01',null,null)`); err != nil {
		t.Fatal(err)
	}
	secondRequest, err := parseAdminReportPageRequest(url.Values{"status": {"pending"}, "limit": {"50"}, "cursor": {first.NextCursor}})
	if err != nil {
		t.Fatal(err)
	}
	before = counter.queries.Load()
	second, err := server.queryAdminReportPage(ctx, secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-before != 1 {
		t.Fatalf("second admin-report page executed %d SQL statements, want 1", counter.queries.Load()-before)
	}
	for _, item := range second.Items {
		if item.ID == "001000002" {
			t.Fatal("concurrent admin-queue head crossed an existing cursor")
		}
		if _, duplicate := seen[item.ID]; duplicate {
			t.Fatalf("duplicate admin report %s across pages", item.ID)
		}
	}
	deep := request
	deep.Cursor = &governancePageCursor{Version: governancePageCursorVersion, Scope: deep.Scope,
		CreatedAt: time.Date(2000, 1, 7, 18, 53, 20, 0, time.UTC), ID: 500000}
	before = counter.queries.Load()
	page, err := server.queryAdminReportPage(ctx, deep)
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-before != 1 || len(page.Items) != 50 {
		t.Fatalf("deep admin-report page queries=%d items=%d", counter.queries.Load()-before, len(page.Items))
	}
	createdAt, id := governancePagePosition(deep)
	plan := governanceScalePlan(t, ctx, pool, adminReportPageSQL, deep.Status, createdAt, id, deep.Limit+1)
	if !strings.Contains(plan, "idx_reports_queue") || strings.Contains(plan, "Seq Scan on reports") {
		t.Fatalf("million-row admin-report page lost its queue index:\n%s", plan)
	}
}

func testBlackroomPageStability(t *testing.T, ctx context.Context, pool *pgxpool.Pool, server *Server, counter *integrationQueryCounter) {
	t.Helper()
	request, err := parsePublicBlackroomPageRequest(url.Values{"limit": {"50"}})
	if err != nil {
		t.Fatal(err)
	}
	before := counter.queries.Load()
	first, err := server.queryPublicBlackroomPage(ctx, request, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-before != 1 || len(first.Items) != 50 || first.NextCursor == "" {
		t.Fatalf("first blackroom page queries=%d items=%d cursor=%t", counter.queries.Load()-before, len(first.Items), first.NextCursor != "")
	}
	seen := blackroomIDs(first.Items)
	if _, err = pool.Exec(ctx, `insert into ban_records values(1000001,'001000001',1,'user-1','','spam','','active',timestamptz '2100-01-01',null,null,timestamptz '2100-01-01')`); err != nil {
		t.Fatal(err)
	}
	secondRequest, err := parsePublicBlackroomPageRequest(url.Values{"limit": {"50"}, "cursor": {first.NextCursor}})
	if err != nil {
		t.Fatal(err)
	}
	before = counter.queries.Load()
	second, err := server.queryPublicBlackroomPage(ctx, secondRequest, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-before != 1 {
		t.Fatalf("second blackroom page executed %d SQL statements, want 1", counter.queries.Load()-before)
	}
	for _, item := range second.Items {
		if item.ID == "001000001" {
			t.Fatal("concurrent blackroom head crossed an existing cursor")
		}
		if _, duplicate := seen[item.ID]; duplicate {
			t.Fatalf("duplicate blackroom record %s across pages", item.ID)
		}
	}
	deep := request
	deep.Cursor = &governancePageCursor{Version: governancePageCursorVersion, Scope: deep.Scope,
		CreatedAt: time.Date(2000, 1, 7, 18, 53, 20, 0, time.UTC), ID: 500000}
	before = counter.queries.Load()
	page, err := server.queryPublicBlackroomPage(ctx, deep, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-before != 1 || len(page.Items) != 50 {
		t.Fatalf("deep blackroom page queries=%d items=%d", counter.queries.Load()-before, len(page.Items))
	}
	createdAt, id := governancePagePosition(deep)
	plan := governanceScalePlan(t, ctx, pool, blackroomPageSQL, createdAt, id, deep.Limit+1)
	if !strings.Contains(plan, "idx_ban_records_public") || strings.Contains(plan, "Seq Scan on ban_records") {
		t.Fatalf("million-row blackroom page lost its history index:\n%s", plan)
	}
}

func reportIDs(items []ownReportListItem) map[string]struct{} {
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		result[item.ID] = struct{}{}
	}
	return result
}

func adminReportIDs(items []adminReportListItem) map[string]struct{} {
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		result[item.ID] = struct{}{}
	}
	return result
}

func blackroomIDs(items []blackroomListItem) map[string]struct{} {
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		result[item.ID] = struct{}{}
	}
	return result
}

func governanceScalePlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) string {
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
