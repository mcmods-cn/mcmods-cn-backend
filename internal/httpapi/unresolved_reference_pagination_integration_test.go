package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestUnresolvedReferenceProjectionPagesStayBoundedAtOneMillionRowsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate million-row unresolved reference pages")
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
	if err = pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	fixtureStarted := time.Now()
	if _, err = pool.Exec(ctx, `
		create temporary table unresolved_reference_catalog (
			origin smallint not null,source_row_id bigint not null,source_type text not null,source_id bigint not null,
			field_path text not null,reference_type text not null,raw_identifier text not null,status text not null,
			resolved_type text not null,resolved_id text not null,source_label text not null,source_public_id text not null,
			created_at timestamptz not null,resolved_at timestamptz,primary key(origin,source_row_id)
		);
		create index idx_unresolved_reference_catalog_page
			on unresolved_reference_catalog(created_at desc,origin desc,source_row_id desc);
		create index idx_unresolved_reference_catalog_status_page
			on unresolved_reference_catalog(status,created_at desc,origin desc,source_row_id desc);
		create index idx_unresolved_reference_catalog_type_page
			on unresolved_reference_catalog(reference_type,created_at desc,origin desc,source_row_id desc);
		create index idx_unresolved_reference_catalog_status_type_page
			on unresolved_reference_catalog(status,reference_type,created_at desc,origin desc,source_row_id desc);
		create index idx_unresolved_reference_catalog_raw_prefix
			on unresolved_reference_catalog(lower(raw_identifier) text_pattern_ops);
		create index idx_unresolved_reference_catalog_label_prefix
			on unresolved_reference_catalog(lower(source_label) text_pattern_ops);
		insert into unresolved_reference_catalog
		select (value%2)::smallint,value,'scale',value,'field','mod',
			case when value%100=0 then 'needle059-'||lpad(value::text,9,'0') else 'bulk-'||lpad(value::text,9,'0') end,
			'pending','','','source-'||value,'',
			timestamptz '2026-01-01 00:00:00+00'+value*interval '1 millisecond',null
		from generate_series(1,1000000) value;
		analyze unresolved_reference_catalog
	`); err != nil {
		t.Fatal(err)
	}
	t.Logf("1m unresolved reference projection rows loaded in %s", time.Since(fixtureStarted))

	server := &Server{db: pool}
	request, err := parseUnresolvedReferencePageRequest(url.Values{
		"type": {"mod"}, "status": {"pending"}, "limit": {"50"},
	})
	if err != nil {
		t.Fatal(err)
	}
	before := counter.queries.Load()
	first, err := server.queryUnresolvedReferencePage(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-before != 1 || len(first.Items) != 50 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page queries=%d items=%d hasMore=%t cursor=%t",
			counter.queries.Load()-before, len(first.Items), first.HasMore, first.NextCursor != "")
	}
	seen := make(map[string]struct{}, len(first.Items))
	for _, item := range first.Items {
		seen[item.ID] = struct{}{}
	}
	if _, err = pool.Exec(ctx, `insert into unresolved_reference_catalog values(
		1,1000001,'scale',1000001,'field','mod','concurrent-head','pending','','','source','',
		timestamptz '2100-01-01 00:00:00+00',null)`); err != nil {
		t.Fatal(err)
	}
	secondRequest, err := parseUnresolvedReferencePageRequest(url.Values{
		"type": {"mod"}, "status": {"pending"}, "limit": {"50"}, "cursor": {first.NextCursor},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := server.queryUnresolvedReferencePage(ctx, secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range second.Items {
		if item.ID == "resource:1000001" {
			t.Fatal("concurrent head crossed an existing unresolved-reference cursor")
		}
		if _, duplicate := seen[item.ID]; duplicate {
			t.Fatalf("duplicate unresolved reference %s across pages", item.ID)
		}
	}

	deepRequest := request
	deepRequest.Cursor = &unresolvedReferencePageCursor{
		Version: unresolvedReferencePageCursorVersion, Scope: request.Scope,
		CreatedAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Add(1001 * time.Millisecond),
		Origin:    1, SourceRowID: 1001,
	}
	pageSQL, pageArgs := buildUnresolvedReferencePageQuery(deepRequest)
	deepPlan := unresolvedReferenceScalePlan(t, ctx, pool, pageSQL, pageArgs...)
	if !strings.Contains(deepPlan, "idx_unresolved_reference_catalog_status_type_page") &&
		!strings.Contains(deepPlan, "idx_unresolved_reference_catalog_page") ||
		strings.Contains(deepPlan, "Seq Scan on unresolved_reference_catalog") {
		t.Fatalf("million-row deep page lost its keyset index:\n%s", deepPlan)
	}

	searchRequest, err := parseUnresolvedReferencePageRequest(url.Values{
		"q": {"needle059"}, "type": {"mod"}, "status": {"pending"}, "limit": {"50"},
	})
	if err != nil {
		t.Fatal(err)
	}
	before = counter.queries.Load()
	searchPage, err := server.queryUnresolvedReferencePage(ctx, searchRequest)
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-before != 2 || len(searchPage.Items) != 50 || !searchPage.HasMore {
		t.Fatalf("bounded search queries=%d items=%d hasMore=%t",
			counter.queries.Load()-before, len(searchPage.Items), searchPage.HasMore)
	}
	budgetSQL, budgetArgs := buildUnresolvedReferenceSearchBudgetQuery(searchRequest)
	searchPlan := unresolvedReferenceScalePlan(t, ctx, pool, budgetSQL, budgetArgs...)
	if !strings.Contains(searchPlan, "idx_unresolved_reference_catalog_raw_prefix") || strings.Contains(searchPlan, "Seq Scan on unresolved_reference_catalog") {
		t.Fatalf("million-row prefix search lost its raw prefix index:\n%s", searchPlan)
	}

	broadRequest, err := parseUnresolvedReferencePageRequest(url.Values{
		"q": {"bulk"}, "type": {"mod"}, "status": {"pending"}, "limit": {"50"},
	})
	if err != nil {
		t.Fatal(err)
	}
	before = counter.queries.Load()
	if _, err = server.queryUnresolvedReferencePage(ctx, broadRequest); !errors.Is(err, errUnresolvedReferenceSearchTooBroad) {
		t.Fatalf("broad prefix returned err=%v", err)
	}
	if counter.queries.Load()-before != 1 {
		t.Fatalf("broad prefix executed %d SQL statements, want budget probe only", counter.queries.Load()-before)
	}
	t.Log("deep cursor used a page keyset index; selective prefix used the expression index; broad prefix failed after one bounded probe")
}

func TestUnresolvedReferenceCursorPageStaysRecoverableAfterConcurrentDeletionIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify empty unresolved-reference cursor pages")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `
		create temporary table unresolved_reference_catalog (
			origin smallint not null,source_row_id bigint not null,source_type text not null,source_id bigint not null,
			field_path text not null,reference_type text not null,raw_identifier text not null,status text not null,
			resolved_type text not null,resolved_id text not null,source_label text not null,source_public_id text not null,
			created_at timestamptz not null,resolved_at timestamptz,primary key(origin,source_row_id)
		);
		insert into unresolved_reference_catalog values
			(0,1,'source',1,'field','mod','older','pending','','','source','',timestamptz '2026-01-01 00:00:01+00',null),
			(0,2,'source',2,'field','mod','newer','pending','','','source','',timestamptz '2026-01-01 00:00:02+00',null)
	`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	request, err := parseUnresolvedReferencePageRequest(url.Values{"type": {"mod"}, "status": {"pending"}, "limit": {"1"}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := server.queryUnresolvedReferencePage(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page=%#v", first)
	}
	if _, err = pool.Exec(ctx, `delete from unresolved_reference_catalog`); err != nil {
		t.Fatal(err)
	}
	staleRequest, err := parseUnresolvedReferencePageRequest(url.Values{
		"type": {"mod"}, "status": {"pending"}, "limit": {"1"}, "cursor": {first.NextCursor},
	})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := server.queryUnresolvedReferencePage(ctx, staleRequest)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Items) != 0 || empty.HasMore || empty.NextCursor != "" || empty.Limit != 1 {
		t.Fatalf("empty cursor page has contradictory facts: %#v", empty)
	}
}

func unresolvedReferenceScalePlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) string {
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
