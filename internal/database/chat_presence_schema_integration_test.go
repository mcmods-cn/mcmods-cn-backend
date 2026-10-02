package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestChatPresencePostgreSQLTableIsAbsentIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the chat presence schema authority")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
	var publicGenerationBefore int
	if err = pool.QueryRow(ctx, `select generation from public.schema_metadata where singleton`).Scan(&publicGenerationBefore); err != nil {
		t.Fatal(err)
	}
	if err = InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	cleaned := false
	defer func() {
		if !cleaned {
			if dropErr := DropEphemeralSchema(context.Background(), pool); dropErr != nil {
				t.Errorf("drop ephemeral schema: %v", dropErr)
			}
		}
	}()
	var generation int
	var relationExists bool
	if err = pool.QueryRow(ctx, `select generation from schema_metadata where singleton`).Scan(&generation); err != nil || generation != schemaGeneration {
		t.Fatalf("temporary schema generation=%d err=%v", generation, err)
	}
	if err = pool.QueryRow(ctx, `select to_regclass(current_schema() || '.user_chat_presence') is not null`).Scan(&relationExists); err != nil {
		t.Fatal(err)
	}
	if relationExists {
		t.Fatal("temporary schema installed the unused user_chat_presence relation")
	}
	if err = DropEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	cleaned = true
	var publicGenerationAfter int
	if err = pool.QueryRow(ctx, `select generation from public.schema_metadata where singleton`).Scan(&publicGenerationAfter); err != nil {
		t.Fatal(err)
	}
	if publicGenerationAfter != publicGenerationBefore {
		t.Fatalf("public generation changed from %d to %d", publicGenerationBefore, publicGenerationAfter)
	}
}
