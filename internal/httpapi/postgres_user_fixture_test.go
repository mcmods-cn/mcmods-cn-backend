package httpapi

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

// These tests exercise writes, workers or locks on an otherwise empty schema.
// Each gets its own ordinary user and an independently cleaned database.
func newPostgresUserFixture(t *testing.T, ctx context.Context, username string) (*pgxpool.Pool, int64) {
	t.Helper()
	configured := config.Load().DB.ConnString()
	if target := os.Getenv("MCMODS_TEST_DATABASE_URL"); target != "" && target != configured {
		t.Fatal("PostgreSQL fixture requires matching application and test database targets")
	}
	parsed, err := pgxpool.ParseConfig(configured)
	if err != nil {
		t.Fatal("cannot parse PostgreSQL fixture target")
	}
	switch parsed.ConnConfig.Host {
	case "127.0.0.1", "localhost", "::1":
	default:
		t.Fatal("PostgreSQL user fixture requires an owned loopback service")
	}
	pool := newTEST018IsolatedDatabase(t, ctx)
	var userID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values($1,$2,'not-used','active') returning id`, username, username+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("create fixture user: %v", err)
	}
	return pool, userID
}
