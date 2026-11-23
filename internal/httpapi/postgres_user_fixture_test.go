package httpapi

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/tools/testing/postgrestarget"
)

// These tests exercise writes, workers or locks on an otherwise empty schema.
// Each gets its own ordinary user and an independently cleaned database.
func newPostgresUserFixture(t *testing.T, ctx context.Context, username string) (*pgxpool.Pool, int64) {
	t.Helper()
	settings := config.Load().DB
	target := settings.ConnString()
	if testTarget := os.Getenv("MCMODS_TEST_DATABASE_URL"); testTarget != "" {
		if settings.URL != "" && testTarget != target {
			t.Fatal("PostgreSQL fixture requires matching explicit application and test database targets")
		}
		target = testTarget
	}
	parsed, err := postgrestarget.ParseSingleLoopback(target)
	if err != nil {
		t.Fatal(err)
	}
	pool := newTEST018IsolatedDatabaseWithConfig(t, ctx, parsed)
	var userID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values($1,$2,'not-used','active') returning id`, username, username+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("create fixture user: %v", err)
	}
	return pool, userID
}
