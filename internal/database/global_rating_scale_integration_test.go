package database

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestGlobalRatingSetRebuildAtScaleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the global rating rebuild plan at scale")
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
	if _, err = pool.Exec(ctx, `create temporary table public_routes(
		id bigint primary key,entity_type text not null,internal_id bigint not null
	);
	create index idx_perf035_routes on public_routes(entity_type,internal_id,id);
	create temporary table effective_project_access(
		user_id bigint not null,project_type text not null,project_id bigint not null,access_level text not null
	);
	create index idx_perf035_access on effective_project_access(project_type,project_id,access_level,user_id);
	create temporary table users(id bigint primary key,status text not null,security_score integer not null);
	create temporary table content_ratings(
		id bigint not null,object_route_id bigint not null,author_id bigint not null,
		overall_score smallint not null,status text not null
	);
	create index idx_perf035_ratings on content_ratings(object_route_id,status,author_id);
	create temporary table content_rating_global_stats(
		entity_type text primary key,rating_count bigint not null default 0,rating_sum bigint not null default 0,
		average_rating numeric(6,4) not null default 3.5,updated_at timestamptz not null default now()
	);
	insert into public_routes select value,'mod',value from generate_series(1,100) value;
	insert into users values(1,'active',100),(2,'active',100);
	insert into effective_project_access values(1,'mod',1,'developer'),(2,'mod',1,'developer')`); err != nil {
		t.Fatal(err)
	}

	var previousScale int64
	for _, scale := range []int64{100_000, 1_000_000, 10_000_000} {
		command, insertErr := pool.Exec(ctx, `insert into content_ratings(id,object_route_id,author_id,overall_score,status)
			select value,1+(value%100),1+((value/100)%2),4,'published'
			from generate_series($1::bigint+1,$2::bigint) value`, previousScale, scale)
		if insertErr != nil {
			t.Fatalf("populate PERF035 ratings at %d rows: %v", scale, insertErr)
		}
		if command.RowsAffected() != scale-previousScale {
			t.Fatalf("populate PERF035 ratings at %d rows affected=%d", scale, command.RowsAffected())
		}
		if _, err = pool.Exec(ctx, `analyze public_routes; analyze effective_project_access;
			analyze users; analyze content_ratings`); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		var plan string
		if err = pool.QueryRow(ctx, `explain(analyze,buffers,format json) `+contentRatingGlobalRebuildSQL, "mod").Scan(&plan); err != nil {
			t.Fatalf("plan PERF035 global rating rebuild at %d rows: %v", scale, err)
		}
		elapsed := time.Since(started)
		if elapsed > 15*time.Second {
			t.Fatalf("PERF035 global rating rebuild at %d rows took %s", scale, elapsed)
		}
		if strings.Contains(strings.ToLower(plan), "content_target_owner_id") {
			t.Fatalf("PERF035 plan retained per-rating owner function at %d rows: %s", scale, plan)
		}
		var ratingCount, ratingSum int64
		var ratingAverage float64
		if err = pool.QueryRow(ctx, `select rating_count,rating_sum,average_rating::double precision
			from content_rating_global_stats where entity_type='mod'`).Scan(&ratingCount, &ratingSum, &ratingAverage); err != nil {
			t.Fatal(err)
		}
		excluded := scale / 100
		if ratingCount != scale-excluded || ratingSum != 4*(scale-excluded) || ratingAverage != 4 {
			t.Fatalf("PERF035 global facts at %d rows count=%d sum=%d average=%v", scale, ratingCount, ratingSum, ratingAverage)
		}
		t.Logf("PERF035 ratings=%d excludedDevelopers=%d rebuildPlanWall=%s", scale, excluded, elapsed)
		previousScale = scale
	}
}
