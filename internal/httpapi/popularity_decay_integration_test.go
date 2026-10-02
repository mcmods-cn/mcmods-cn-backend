package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestPopularityDecaySchedulerUsesDueIndexAtScaleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the popularity decay plan at scale")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	loaded := config.Load()
	poolConfig, err := pgxpool.ParseConfig(loaded.DB.ConnString())
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
	if _, err = pool.Exec(ctx, `create temporary table content_popularity_stats(
		object_route_id bigint not null,
		next_decay_at timestamptz
	);
	create index idx_content_popularity_stats_decay_due
		on content_popularity_stats(next_decay_at,object_route_id) where next_decay_at is not null;
	create temporary table content_stats_refresh_queue(
		object_route_id bigint primary key,
		status text not null default 'pending',
		refresh_metrics boolean not null default false,
		refresh_popularity boolean not null default false,
		attempts integer not null default 0,
		available_at timestamptz not null default now(),
		locked_at timestamptz,
		updated_at timestamptz not null default now()
	)`); err != nil {
		t.Fatal(err)
	}

	var previousScale int64
	for _, scale := range []int64{100_000, 1_000_000, 10_000_000} {
		command, insertErr := pool.Exec(ctx, `insert into content_popularity_stats(object_route_id,next_decay_at)
			select value,case when value%100000=0 then now()-interval '1 minute' else null end
			from generate_series($1::bigint+1,$2::bigint) value`, previousScale, scale)
		if insertErr != nil {
			t.Fatalf("populate PERF034 decay schedule at %d rows: %v", scale, insertErr)
		}
		if command.RowsAffected() != scale-previousScale {
			t.Fatalf("populate PERF034 decay schedule at %d rows affected=%d", scale, command.RowsAffected())
		}
		if _, err = pool.Exec(ctx, `analyze content_popularity_stats; truncate content_stats_refresh_queue`); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		var plan string
		if err = pool.QueryRow(ctx, `explain(analyze,buffers,format json) `+popularityDueStatsEnqueueSQL).Scan(&plan); err != nil {
			t.Fatalf("plan PERF034 decay scheduler at %d rows: %v", scale, err)
		}
		elapsed := time.Since(started)
		lowerPlan := strings.ToLower(plan)
		if !strings.Contains(lowerPlan, "idx_content_popularity_stats_decay_due") || strings.Contains(lowerPlan, `"node type": "seq scan"`) {
			t.Fatalf("PERF034 decay scheduler missed due index at %d rows: %s", scale, plan)
		}
		var queued int64
		if err = pool.QueryRow(ctx, `select count(*) from content_stats_refresh_queue`).Scan(&queued); err != nil {
			t.Fatal(err)
		}
		if expected := scale / 100_000; queued != expected {
			t.Fatalf("PERF034 decay scheduler queued=%d want=%d at %d rows", queued, expected, scale)
		}
		t.Logf("PERF034 decayRows=%d due=%d enqueuePlanWall=%s", scale, queued, elapsed)
		previousScale = scale
	}
}
