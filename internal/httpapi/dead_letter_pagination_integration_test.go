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
)

func TestAdminDeadLettersTraversesFilteredHistoryIntegration(t *testing.T) {
	pool, ctx := openDeadLetterPageTestDB(t, 30*time.Second)
	installDeadLetterPageTable(t, ctx, pool)
	if _, err := pool.Exec(ctx, `insert into dead_letter_events(
		id,event_id,event_type,subject,payload,failure_stage,aggregate_type,aggregate_id,attempts,last_error,failed_at,replayed_at
	) select value,'target-'||value,'project.failed','projects','{}','publish','mod','perf038',3,'failed',
		timestamptz '2025-02-03 04:05:06+00',null from generate_series(1,150) value;
	insert into dead_letter_events(
		id,event_id,event_type,subject,payload,failure_stage,aggregate_type,aggregate_id,attempts,last_error,failed_at,replayed_at
	) select 1000+value,'other-'||value,'project.failed','projects','{}','publish','mod','other',3,'failed',
		timestamptz '2025-02-03 04:05:06+00',null from generate_series(1,30) value;
	insert into dead_letter_events(
		id,event_id,event_type,subject,payload,failure_stage,aggregate_type,aggregate_id,attempts,last_error,failed_at,replayed_at
	) select 2000+value,'replayed-'||value,'project.failed','projects','{}','publish','mod','perf038',3,'failed',
		timestamptz '2025-02-03 04:05:06+00',now() from generate_series(1,20) value`); err != nil {
		t.Fatal(err)
	}
	var targetCount int
	if err := pool.QueryRow(ctx, `select count(*) from dead_letter_events
		where replayed_at is null and aggregate_type='mod' and aggregate_id='perf038'`).Scan(&targetCount); err != nil || targetCount != 150 {
		t.Fatalf("seeded target dead letters = %d, error = %v", targetCount, err)
	}
	server := &Server{db: pool}
	base := "/api/v1/admin/infrastructure/dead-letters?status=unresolved&aggregateType=mod&aggregateId=perf038&limit=100"
	first := requestDeadLetterPage(t, server, base)
	if len(first.Items) != 100 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first dead-letter page = items %d hasMore %t cursor %q", len(first.Items), first.HasMore, first.NextCursor)
	}
	second := requestDeadLetterPage(t, server, base+"&cursor="+url.QueryEscape(first.NextCursor))
	if len(second.Items) != 50 || second.HasMore || second.NextCursor != "" {
		t.Fatalf("second dead-letter page = items %d hasMore %t cursor %q", len(second.Items), second.HasMore, second.NextCursor)
	}
	seen := make(map[int64]bool, 150)
	for _, item := range append(first.Items, second.Items...) {
		if seen[item.ID] || item.AggregateID != "perf038" || item.Status != "unresolved" {
			t.Fatalf("invalid or duplicate dead-letter item: %#v", item)
		}
		seen[item.ID] = true
	}
	if len(seen) != 150 || !seen[1] || !seen[150] {
		t.Fatalf("dead-letter history coverage = %d, first=%t last=%t", len(seen), seen[1], seen[150])
	}
}

func TestDeadLetterPagesStayIndexedAtScaleIntegration(t *testing.T) {
	pool, ctx := openDeadLetterPageTestDB(t, 2*time.Minute)
	installDeadLetterPageTable(t, ctx, pool)
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	inserted := 0
	for _, scale := range []int{100_000, 1_000_000} {
		if _, err := pool.Exec(ctx, `insert into dead_letter_events(
			id,event_id,event_type,subject,payload,failure_stage,aggregate_type,aggregate_id,
			attempts,last_error,failed_at,replayed_at
		) select value,'event-'||value,'project.failed','projects','{}','publish','mod',
			case when value%1000=0 then 'target' else 'other' end,3,'failed',
			timestamptz '2025-01-01 00:00:00+00'+value*interval '1 microsecond',
			case when value%2=0 then timestamptz '2025-02-01 00:00:00+00' else null end
		from generate_series($1::bigint,$2::bigint) value`, inserted+1, scale); err != nil {
			t.Fatal(err)
		}
		inserted = scale
		if _, err := pool.Exec(ctx, `analyze dead_letter_events`); err != nil {
			t.Fatal(err)
		}
		for name, page := range map[string]deadLetterPageRequest{
			"unresolved": {Status: "unresolved", Limit: 100, AfterFailedAt: baseTime.Add(time.Duration(scale/2) * time.Microsecond), AfterID: int64(scale / 2)},
			"replayed":   {Status: "replayed", Limit: 100, AfterFailedAt: baseTime.Add(time.Duration(scale/2) * time.Microsecond), AfterID: int64(scale / 2)},
			"aggregate":  {Status: "all", AggregateType: "mod", AggregateID: "target", Limit: 100, AfterFailedAt: baseTime.Add(time.Duration(scale/2) * time.Microsecond), AfterID: int64(scale / 2)},
		} {
			query, arguments := deadLetterPageSQL(page)
			started := time.Now()
			rows, err := pool.Query(ctx, `explain (analyze,format text) `+query, arguments...)
			if err != nil {
				t.Fatal(err)
			}
			var plan string
			for rows.Next() {
				var line string
				if err = rows.Scan(&line); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				plan += line + "\n"
			}
			rows.Close()
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(plan, "Seq Scan") || (!strings.Contains(plan, "Index Scan") && !strings.Contains(plan, "Index Only Scan")) {
				t.Fatalf("PERF038 %d %s plan is not indexed:\n%s", scale, name, plan)
			}
			if elapsed := time.Since(started); elapsed > 2*time.Second {
				t.Fatalf("PERF038 %d %s page took %s", scale, name, elapsed)
			} else {
				t.Logf("PERF038 %d %s page plan wall=%s", scale, name, elapsed)
			}
		}
	}
}

type deadLetterPageResponse struct {
	Items      []deadLetterPageItem `json:"items"`
	Limit      int                  `json:"limit"`
	HasMore    bool                 `json:"hasMore"`
	NextCursor string               `json:"nextCursor"`
}

func requestDeadLetterPage(t *testing.T, server *Server, path string) deadLetterPageResponse {
	t.Helper()
	response := httptest.NewRecorder()
	server.adminDeadLetters(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s returned %d: %s", path, response.Code, response.Body.String())
	}
	var envelope struct {
		Data deadLetterPageResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func openDeadLetterPageTestDB(t *testing.T, timeout time.Duration) (*pgxpool.Pool, context.Context) {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify dead-letter pagination")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
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
	t.Cleanup(pool.Close)
	return pool, ctx
}

func installDeadLetterPageTable(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `create temporary table dead_letter_events(
		id bigint primary key,event_id text not null,event_type text not null,subject text not null,payload jsonb not null,
		failure_stage text not null,aggregate_type text not null default '',aggregate_id text not null default '',
		attempts bigint not null,last_error text not null,failed_at timestamptz not null,replayed_at timestamptz
	);
	create index idx_dead_letter_events_page on dead_letter_events(failed_at desc,id desc);
	create index idx_dead_letter_events_pending on dead_letter_events(failed_at desc,id desc) where replayed_at is null;
	create index idx_dead_letter_events_replayed on dead_letter_events(failed_at desc,id desc) where replayed_at is not null;
	create index idx_dead_letter_events_aggregate on dead_letter_events(aggregate_type,aggregate_id,failed_at desc,id desc)`); err != nil {
		t.Fatal(err)
	}
}
