package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestShowcaseQueriesReleaseSingleConnectionBeforeResolvingImagesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an owned test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	}()
	server := &Server{db: pool}
	for _, test := range []struct {
		name string
		load func(context.Context, int64) ([]userShowcaseItem, error)
	}{
		{"claimed authors", server.loadUserClaimedAuthors},
		{"projects", server.loadUserShowcaseProjects},
		{"uploads", server.loadUserShowcaseUploads},
	} {
		t.Run(test.name, func(t *testing.T) {
			queryCtx, queryCancel := context.WithTimeout(ctx, 2*time.Second)
			defer queryCancel()
			if _, err := test.load(queryCtx, 123456); err != nil {
				t.Fatalf("single-connection showcase failed: %v", err)
			}
			if err := queryCtx.Err(); err != nil {
				t.Fatalf("image resolution exhausted its context while holding the only connection: %v", err)
			}
		})
	}
}
