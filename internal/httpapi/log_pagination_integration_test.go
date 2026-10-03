package httpapi

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestLogPaginationSearchAndCleanupStayIndexedAtScaleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate million-row log plans")
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
	if err = pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	if _, err = pool.Exec(ctx, `
		create temp table users(id bigint primary key,public_id text,username text,email text);
		create temp table oss_files(id bigint primary key,public_id text);
		create temp table app_logs(
			id bigint primary key,category text,level text,actor_id bigint,action text,target text,ip text,user_agent text,
			method text,path text,status integer,latency_ms bigint,payload jsonb,search_document tsvector,created_at timestamptz
		);
		insert into app_logs
		select gs,'api_access','error',null,'GET','scale','127.0.0.1','scale-agent','GET','/scale',500,1,'{}'::jsonb,
			case when gs%10000=0 then to_tsvector('simple','perf033needle') else ''::tsvector end,
			timestamptz '2025-01-01 00:00:00+00'+gs*interval '1 millisecond'
		from generate_series(1,1000000) gs;
		create index idx_log_scale_app_page on app_logs(category,created_at desc,id desc);
		create index idx_log_scale_app_search on app_logs using gin(search_document);

		create temp table permission_audit_logs(
			id bigint primary key,operator_id bigint,target_user_id bigint,action text,payload jsonb,search_document tsvector,created_at timestamptz
		);
		insert into permission_audit_logs
		select gs,null,null,'change','{}'::jsonb,
			case when gs%1000=0 then to_tsvector('simple','perf033needle') else ''::tsvector end,
			timestamptz '2025-01-01 00:00:00+00'+gs*interval '1 millisecond'
		from generate_series(1,100000) gs;
		create index idx_log_scale_permission_page on permission_audit_logs(created_at desc,id desc);
		create index idx_log_scale_permission_search on permission_audit_logs using gin(search_document);

		create temp table user_login_logs(
			id bigint primary key,user_id bigint,account text,ip text,user_agent text,success boolean,reason text,
			search_document tsvector,created_at timestamptz
		);
		insert into user_login_logs
		select gs,null,'scale@example.test','127.0.0.1','scale-agent',false,'failed',
			case when gs%1000=0 then to_tsvector('simple','perf033needle') else ''::tsvector end,
			timestamptz '2025-01-01 00:00:00+00'+gs*interval '1 millisecond'
		from generate_series(1,100000) gs;
		create index idx_log_scale_login_page on user_login_logs(created_at desc,id desc);
		create index idx_log_scale_login_search on user_login_logs using gin(search_document);

		create temp table oss_upload_logs(
			id bigint primary key,file_id bigint,uploader_id bigint,object_key text,original_name text,size_bytes bigint,
			ip text,user_agent text,result text,message text,search_document tsvector,created_at timestamptz
		);
		insert into oss_upload_logs
		select gs,null,null,'scale/object','scale.bin',1,'127.0.0.1','scale-agent','failed','failed',
			case when gs%1000=0 then to_tsvector('simple','perf033needle') else ''::tsvector end,
			timestamptz '2025-01-01 00:00:00+00'+gs*interval '1 millisecond'
		from generate_series(1,100000) gs;
		create index idx_log_scale_upload_page on oss_upload_logs(created_at desc,id desc);
		create index idx_log_scale_upload_search on oss_upload_logs using gin(search_document);
		-- Only these temporary fixtures need a full ANALYZE sample: the default
		-- sample can miss the 1-in-10000 term and estimate 5000 matches instead of
		-- 100. Target 10000 requests 3M rows, exceeding each fixture's population.
		alter table app_logs alter column search_document set statistics 10000;
		alter table permission_audit_logs alter column search_document set statistics 10000;
		alter table user_login_logs alter column search_document set statistics 10000;
		alter table oss_upload_logs alter column search_document set statistics 10000;
		analyze users; analyze oss_files; analyze app_logs; analyze permission_audit_logs; analyze user_login_logs; analyze oss_upload_logs;
	`); err != nil {
		t.Fatal(err)
	}
	loadElapsed := time.Since(started)

	request, err := parseLogPageRequest(url.Values{
		"category": {"api_access"}, "q": {"perf033needle"}, "limit": {"25"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	firstPage, err := server.loadLogPage(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Items) != 25 || !firstPage.HasMore || firstPage.NextCursor == "" {
		t.Fatalf("first page items=%d hasMore=%t cursor=%t", len(firstPage.Items), firstPage.HasMore, firstPage.NextCursor != "")
	}
	secondValues := url.Values{"category": {"api_access"}, "q": {"perf033needle"}, "limit": {"25"}, "cursor": {firstPage.NextCursor}}
	secondRequest, err := parseLogPageRequest(secondValues)
	if err != nil {
		t.Fatal(err)
	}
	secondPage, err := server.loadLogPage(ctx, secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[int64]bool, len(firstPage.Items))
	for _, item := range firstPage.Items {
		id, ok := logRowID(item["id"])
		if !ok {
			t.Fatalf("invalid first-page id %v", item["id"])
		}
		seen[id] = true
	}
	for _, item := range secondPage.Items {
		id, ok := logRowID(item["id"])
		if !ok || seen[id] {
			t.Fatalf("second-page stable key id=%v valid=%t duplicate=%t", item["id"], ok, seen[id])
		}
	}

	pagePlanRequest, err := parseLogPageRequest(url.Values{"category": {"api_access"}, "limit": {"25"}})
	if err != nil {
		t.Fatal(err)
	}
	pagePlanRequest.Cursor = &logPageCursor{CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ID: 2000000}
	pageQuery, pageArgs := logPageSQL(pagePlanRequest)
	pagePlan := logScalePlan(t, ctx, pool, true, pageQuery, pageArgs...)
	if !strings.Contains(pagePlan, "idx_log_scale_app_page") || strings.Contains(pagePlan, "Seq Scan on app_logs") {
		t.Fatalf("million-row application-log page lost its keyset index:\n%s", pagePlan)
	}

	for _, source := range []struct {
		category      string
		table         string
		index         string
		termFrequency float64
	}{
		{category: "api_access", table: "app_logs", index: "idx_log_scale_app_search", termFrequency: 0.0001},
		{category: "permission_change", table: "permission_audit_logs", index: "idx_log_scale_permission_search", termFrequency: 0.001},
		{category: "login_security", table: "user_login_logs", index: "idx_log_scale_login_search", termFrequency: 0.001},
		{category: "file_upload", table: "oss_upload_logs", index: "idx_log_scale_upload_search", termFrequency: 0.001},
	} {
		var commonTerms string
		var termFrequency float64
		if err = pool.QueryRow(ctx, `
			select most_common_elems::text, most_common_elem_freqs[1]
			from pg_stats
			where schemaname=(select nspname from pg_namespace where oid=pg_my_temp_schema())
				and tablename=$1 and attname='search_document'
		`, source.table).Scan(&commonTerms, &termFrequency); err != nil {
			t.Fatalf("%s temporary search statistics: %v", source.table, err)
		}
		if commonTerms != "{perf033needle}" || math.Abs(termFrequency-source.termFrequency) > 1e-8 {
			t.Fatalf("%s temporary search statistics terms=%q frequency=%g; want perf033needle at %g", source.table, commonTerms, termFrequency, source.termFrequency)
		}
		t.Logf("%s temporary search statistics contain perf033needle at frequency %.8f", source.table, termFrequency)
		searchRequest, parseErr := parseLogPageRequest(url.Values{
			"category": {source.category}, "q": {"perf033needle"}, "limit": {"25"},
		})
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		searchRequest.Cursor = &logPageCursor{CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ID: 2000000}
		query, args := logPageSQL(searchRequest)
		plan := logScalePlan(t, ctx, pool, true, query, args...)
		if !strings.Contains(plan, source.index) || strings.Contains(plan, "Seq Scan on "+source.table) {
			t.Errorf("%s search lost its GIN projection:\n%s", source.category, plan)
		}
	}

	var cleanup logCleanupStatement
	for _, statement := range logCleanupStatements(defaultLogConfig(), 1000) {
		if statement.Label == "api_access" {
			cleanup = statement
			break
		}
	}
	if cleanup.SQL == "" {
		t.Fatal("api_access cleanup statement is missing")
	}
	cleanupPlan := logScalePlan(t, ctx, pool, false, cleanup.SQL, cleanup.Args...)
	if !strings.Contains(cleanupPlan, "idx_log_scale_app_page") || strings.Contains(cleanupPlan, "Seq Scan on app_logs") {
		t.Fatalf("million-row cleanup lost its stable batch index:\n%s", cleanupPlan)
	}
	tag, err := pool.Exec(ctx, cleanup.SQL, cleanup.Args...)
	if err != nil {
		t.Fatal(err)
	}
	if tag.RowsAffected() != 1000 {
		t.Fatalf("bounded cleanup deleted %d rows", tag.RowsAffected())
	}
	t.Logf("loaded 1.3M temporary log rows in %s; two search pages were disjoint and cleanup deleted exactly 1000 rows", loadElapsed)
}

func logScalePlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, analyze bool, query string, args ...any) string {
	t.Helper()
	options := "buffers,format text"
	if analyze {
		options = "analyze," + options
	}
	rows, err := pool.Query(ctx, fmt.Sprintf("explain (%s) %s", options, query), args...)
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
