package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
)

func TestCurrentUserFeaturesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an isolated PostgreSQL database to execute migration integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate current schema: %v", err)
	}
	var generation int
	if err = pool.QueryRow(ctx, `select generation from schema_metadata where singleton`).Scan(&generation); err != nil || generation != 80 {
		t.Fatalf("unexpected schema generation %d: %v", generation, err)
	}
	for _, relation := range []string{
		"user_presence_sessions", "user_statistics_daily", "user_statistics_totals",
		"user_content_creation_facts", "activity_cleanup_runs", "activity_event_outbox",
	} {
		var exists bool
		if err = pool.QueryRow(ctx, `select to_regclass('public.'||$1) is not null`, relation).Scan(&exists); err != nil || !exists {
			t.Fatalf("required relation %s missing: %v", relation, err)
		}
	}
	var privateByDefault bool
	if err = pool.QueryRow(ctx, `select column_default='false' from information_schema.columns
		where table_schema='public' and table_name='users' and column_name='show_online_status'`).Scan(&privateByDefault); err != nil || !privateByDefault {
		t.Fatalf("online status privacy default is not installed: %v", err)
	}
	var triggerExists bool
	if err = pool.QueryRow(ctx, `select exists(select 1 from pg_trigger where tgname='trg_user_activity_statistics' and not tgisinternal)`).Scan(&triggerExists); err != nil || !triggerExists {
		t.Fatalf("statistics trigger missing: %v", err)
	}
}
