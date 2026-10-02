package database

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"mcmods-cn-backend/internal/config"
)

func TestConnectDoesNotExposeCredentialsFromInvalidURL(t *testing.T) {
	const fixtureSecret = "synthetic-database-password"
	for _, invalidURL := range []string{
		"postgres://fixture-user:" + fixtureSecret + "@127.0.0.1:invalid/fixture",
		"postgres://fixture-user:" + fixtureSecret + "%@127.0.0.1/fixture",
	} {
		cfg := config.Config{DB: config.DBConfig{URL: invalidURL, MaxConns: 1}}
		pool, err := Connect(context.Background(), cfg)
		if pool != nil {
			pool.Close()
			t.Fatal("invalid database URL returned a pool")
		}
		if err == nil {
			t.Fatal("invalid database URL returned no error")
		}
		if strings.Contains(err.Error(), fixtureSecret) || strings.Contains(err.Error(), "fixture-user") {
			t.Error("database connection error exposed a connection-string credential")
		}
		var parseErr *pgconn.ParseConfigError
		if !errors.As(err, &parseErr) {
			t.Error("database connection error lost the parser error type")
		}
	}
}

func TestConnectionErrorsKeepSafeDiagnosticsAndOriginalCause(t *testing.T) {
	for _, test := range []struct {
		name    string
		cause   error
		message string
	}{
		{"cancelled", context.Canceled, "connect to database: context canceled"},
		{"deadline", context.DeadlineExceeded, "connect to database: deadline exceeded"},
		{"authentication", &pgconn.PgError{Code: "28P01", Message: "synthetic-sensitive-server-message"}, "connect to database: PostgreSQL SQLSTATE 28P01"},
		{"invalid code", &pgconn.PgError{Code: "sensitive\ncode", Message: "synthetic-sensitive-server-message"}, "connect to database: *pgconn.PgError"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := &connectionError{stage: "connect to database", cause: test.cause}
			if err.Error() != test.message {
				t.Error("connection error did not retain safe diagnostic classification")
			}
			if !errors.Is(err, test.cause) {
				t.Error("connection error lost its original cause")
			}
		})
	}
}
