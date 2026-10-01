package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestConfiguredRoleLookupReusesTransactionConnectionIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	cfg, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns, cfg.MinConns = 1, 0
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	// The transaction occupies the only pool connection. A settings read using
	// the pool would wait for itself until the request expires. User 0 does not
	// exist and the DELETE cannot modify another fixture's role bindings.
	if err = (&Server{db: pool}).removeConfiguredRoleTx(ctx, tx, 0, "banned"); err != nil {
		t.Fatalf("role lookup required an extra pool connection: %v", err)
	}
}

func TestPermissionDefaultsDatabaseFailureIsNotEmptySuccessIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	pool, err := pgxpool.New(context.Background(), config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	response := httptest.NewRecorder()
	(&Server{db: pool}).getPermissionDefaults(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/permissions/defaults", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed settings read appeared successful: status=%d body=%s", response.Code, response.Body.String())
	}
}
