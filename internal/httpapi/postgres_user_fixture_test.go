package httpapi

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
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
	parsed, err := parsePostgresUserFixtureTarget(target)
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

func parsePostgresUserFixtureTarget(target string) (*pgxpool.Config, error) {
	parsed, err := pgxpool.ParseConfig(target)
	if err != nil {
		return nil, errors.New("cannot parse PostgreSQL fixture target")
	}
	switch parsed.ConnConfig.Host {
	case "127.0.0.1", "localhost", "::1":
	default:
		return nil, errors.New("PostgreSQL user fixture requires an owned loopback service")
	}
	for _, fallback := range parsed.ConnConfig.Fallbacks {
		if fallback.Host != parsed.ConnConfig.Host || fallback.Port != parsed.ConnConfig.Port {
			return nil, errors.New("PostgreSQL user fixture requires a single database endpoint")
		}
	}
	return parsed, nil
}

func TestPostgresUserFixtureKeepsOneLoopbackTarget(t *testing.T) {
	for _, scenario := range []struct {
		name, target string
		allowed      bool
	}{
		{"same endpoint TLS fallback", "127.0.0.1:5432", true},
		{"different loopback service", "127.0.0.1:5432,127.0.0.1:5433", false},
		{"non-loopback service", "database.example.invalid:5432", false},
		{"non-loopback fallback", "127.0.0.1:5432,database.example.invalid:5432", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, err := parsePostgresUserFixtureTarget("postgres://test@" + scenario.target + "/test_fixture?sslmode=prefer")
			if (err == nil) != scenario.allowed {
				t.Fatalf("allowed=%v, error=%v", scenario.allowed, err)
			}
		})
	}
}
