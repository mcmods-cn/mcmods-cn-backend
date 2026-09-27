package database

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
)

func TestRefreshContentPopularityThresholdLookupIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to run the popularity function integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns, poolConfig.MinConns = 1, 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	suffix := time.Now().UnixNano()
	projectCode := fmt.Sprintf("p%08d", suffix%100_000_000)
	var modID int64
	if err = tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values($1,$2,'Popularity integration','approved') returning id`,
		projectCode, fmt.Sprintf("popularity-integration-%d", suffix)).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	var routeID int64
	if err = tx.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, modID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `select pg_temp.refresh_content_popularity($1::bigint)`, routeID); err != nil {
		t.Fatalf("refresh popularity: %v", err)
	}
	var count int
	if err = tx.QueryRow(ctx, `select count(*) from content_popularity_stats where object_route_id=$1`, routeID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one popularity snapshot, got %d", count)
	}
}
