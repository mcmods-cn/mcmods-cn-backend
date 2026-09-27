package httpapi

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestSeedCrawlerKeysetPagesStayBoundedAtScaleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate seed crawler keyset scale")
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
		create temporary table seed_crawler_runs (
			id bigint primary key,public_id text not null,status text not null,dry_run boolean not null,
			attempts integer not null,stats jsonb not null,last_error text not null,created_at timestamptz not null,
			started_at timestamptz,finished_at timestamptz
		);
		create index idx_seed_crawler_runs_created on seed_crawler_runs(created_at desc,id desc);
		create temporary table seed_crawler_candidates (
			id bigint primary key,first_seen_run_id bigint,last_seen_run_id bigint,external_project_id text not null,project_type text not null,downloads bigint not null,
			status text not null,payload jsonb not null,last_error text not null,created_at timestamptz not null,updated_at timestamptz not null
		);
		create index idx_seed_crawler_candidates_downloads on seed_crawler_candidates(downloads desc,id desc);
		create index idx_seed_crawler_candidates_status_downloads on seed_crawler_candidates(status,downloads desc,id desc);
		create temporary table user_drafts (
			id bigint primary key,public_id text not null,draft_key text not null,submitted_at timestamptz,updated_at timestamptz not null
		);
		create index idx_user_drafts_seed_active on user_drafts(draft_key,updated_at desc,id desc) where submitted_at is null;
		insert into seed_crawler_runs
		select value,'r'||lpad(value::text,8,'0'),case when value%2=0 then 'completed' else 'failed' end,false,
			1,'{}'::jsonb,'',timestamptz '2026-01-01 00:00:00+00'+value*interval '1 second',null,null
		from generate_series(1,100000) value;
		insert into seed_crawler_candidates
		select value,1+((value-1)%100000),1+((value-1)%100000),'p'||lpad(value::text,7,'0'),'mod',value,
			case when value%100=0 then 'failed' when value%10=1 then 'existing' when value%10=2 then 'draft' when value%10=3 then 'submitted' else 'candidate' end,
			jsonb_build_object('label','small-'||value),'',timestamptz '2026-01-01 00:00:00+00',timestamptz '2026-01-01 00:00:00+00'
		from generate_series(1,1000000) value;
		update seed_crawler_candidates set payload=jsonb_build_object('secret',repeat('SECRET-TAIL-',100000)) where id=1000000;
		analyze seed_crawler_runs;
		analyze seed_crawler_candidates;
		analyze user_drafts
	`); err != nil {
		t.Fatal(err)
	}
	t.Logf("100k run / 1m candidate fixture loaded in %s", time.Since(fixtureStarted))

	counter.queries.Store(0)
	runCursor := ""
	seenRuns := make(map[string]struct{}, 100)
	for pageNumber := 0; pageNumber < 2; pageNumber++ {
		request, parseErr := parseSeedCrawlerRunPageRequest(url.Values{"limit": {"50"}, "cursor": {runCursor}})
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		query, args := seedCrawlerRunPageSQL(request)
		rows, queryErr := pool.Query(ctx, query, args...)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		items := make([]seedCrawlerRunSummary, 0, request.Limit+1)
		for rows.Next() {
			var item seedCrawlerRunSummary
			if scanErr := rows.Scan(&item.InternalID, &item.ID, &item.Status, &item.DryRun, &item.Attempts, &item.Stats,
				&item.LastError, &item.CreatedAt, &item.StartedAt, &item.FinishedAt); scanErr != nil {
				rows.Close()
				t.Fatal(scanErr)
			}
			items = append(items, item)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			rows.Close()
			t.Fatal(rowsErr)
		}
		rows.Close()
		if len(items) != 51 {
			t.Fatalf("run page %d fetched %d rows, want limit+1", pageNumber, len(items))
		}
		items = items[:50]
		for _, item := range items {
			if _, duplicate := seenRuns[item.ID]; duplicate {
				t.Fatalf("duplicate run %s", item.ID)
			}
			seenRuns[item.ID] = struct{}{}
		}
		last := items[len(items)-1]
		runCursor = encodeSeedCrawlerRunPageCursor(seedCrawlerRunPageCursor{
			Version: seedCrawlerCursorVersion, Scope: request.Scope, CreatedAt: last.CreatedAt, ID: last.InternalID,
		})
	}
	if queries := counter.queries.Load(); queries != 2 {
		t.Fatalf("two run pages executed %d SQL statements, want 2", queries)
	}

	counter.queries.Store(0)
	candidateCursor := ""
	seenCandidates := make(map[string]struct{}, 100)
	for pageNumber := 0; pageNumber < 2; pageNumber++ {
		request, parseErr := parseSeedCrawlerCandidatePageRequest(url.Values{"limit": {"50"}, "cursor": {candidateCursor}})
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		query, args := seedCrawlerCandidatePageSQL(request)
		rows, queryErr := pool.Query(ctx, query, args...)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		items := make([]seedCrawlerCandidateSummary, 0, request.Limit+1)
		for rows.Next() {
			var item seedCrawlerCandidateSummary
			if scanErr := rows.Scan(&item.InternalID, &item.ExternalProjectID, &item.ProjectType, &item.Downloads, &item.Status,
				&item.FirstSeenRunID, &item.LastSeenRunID, &item.LastError, &item.CreatedAt, &item.UpdatedAt, &item.DraftID); scanErr != nil {
				rows.Close()
				t.Fatal(scanErr)
			}
			items = append(items, item)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			rows.Close()
			t.Fatal(rowsErr)
		}
		rows.Close()
		if len(items) != 51 {
			t.Fatalf("candidate page %d fetched %d rows, want limit+1", pageNumber, len(items))
		}
		items = items[:50]
		encoded, marshalErr := json.Marshal(items)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if len(encoded) >= 128*1024 || strings.Contains(string(encoded), "SECRET-TAIL") || strings.Contains(string(encoded), "payload") {
			t.Fatalf("candidate summary leaked payload or exceeded its byte bound: %d bytes", len(encoded))
		}
		for _, item := range items {
			if _, duplicate := seenCandidates[item.ExternalProjectID]; duplicate {
				t.Fatalf("duplicate candidate %s", item.ExternalProjectID)
			}
			seenCandidates[item.ExternalProjectID] = struct{}{}
		}
		last := items[len(items)-1]
		candidateCursor = encodeSeedCrawlerCandidatePageCursor(seedCrawlerCandidatePageCursor{
			Version: seedCrawlerCursorVersion, Scope: request.Scope, Downloads: last.Downloads, ID: last.InternalID,
		})
	}
	if queries := counter.queries.Load(); queries != 2 {
		t.Fatalf("two candidate pages executed %d SQL statements, want 2", queries)
	}

	var detail seedCrawlerCandidateDetailResponse
	if err = pool.QueryRow(ctx, seedCrawlerCandidateDetailSQL, "p1000000").Scan(
		&detail.ExternalProjectID, &detail.ProjectType, &detail.Downloads, &detail.Status,
		&detail.FirstSeenRunID, &detail.LastSeenRunID, &detail.Payload,
		&detail.LastError, &detail.CreatedAt, &detail.UpdatedAt, &detail.DraftID,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(detail.Payload), "SECRET-TAIL") {
		t.Fatal("single-candidate detail did not return the payload")
	}

	runPlanSQL, runPlanArgs := seedCrawlerRunPageSQL(seedCrawlerRunPageRequest{Limit: 50, Cursor: &seedCrawlerRunPageCursor{
		CreatedAt: time.Date(2026, time.January, 2, 3, 46, 40, 0, time.UTC), ID: 100000,
	}})
	assertSeedCrawlerKeysetPlan(t, ctx, pool, "run 100k depth", "idx_seed_crawler_runs_created", runPlanSQL, runPlanArgs)
	candidatePlanSQL, candidatePlanArgs := seedCrawlerCandidatePageSQL(seedCrawlerCandidatePageRequest{
		Limit: 50, Cursor: &seedCrawlerCandidatePageCursor{Downloads: 500000, ID: 500000},
	})
	assertSeedCrawlerKeysetPlan(t, ctx, pool, "candidate 1m depth", "idx_seed_crawler_candidates_downloads", candidatePlanSQL, candidatePlanArgs)
	statusPlanSQL, statusPlanArgs := seedCrawlerCandidatePageSQL(seedCrawlerCandidatePageRequest{
		Status: "failed", Limit: 50, Cursor: &seedCrawlerCandidatePageCursor{Downloads: 500000, ID: 500000},
	})
	assertSeedCrawlerKeysetPlan(t, ctx, pool, "candidate status 1m depth", "idx_seed_crawler_candidates_status_downloads", statusPlanSQL, statusPlanArgs)
}

func assertSeedCrawlerKeysetPlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name, index string, query string, args []any) {
	t.Helper()
	rows, err := pool.Query(ctx, "explain (analyze,buffers,format text) "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	lines := make([]string, 0)
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	plan := strings.Join(lines, "\n")
	if !strings.Contains(plan, index) || strings.Contains(plan, "Seq Scan on seed_crawler") {
		t.Fatalf("%s did not use %s:\n%s", name, index, plan)
	}
	t.Logf("%s plan:\n%s", name, plan)
}
