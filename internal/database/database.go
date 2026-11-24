package database

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func Connect(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	return connectPool(ctx, cfg.DB, cfg.DB.MinConns, cfg.DB.MaxConns, "mcmods-cn-backend")
}

// ConnectActivity gives activity ingestion a small, explicit connection
// budget. A view burst can no longer consume every connection needed by
// user-facing requests, while PostgreSQL still remains the single authority.
func ConnectActivity(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	return connectPool(ctx, cfg.DB, cfg.Activity.DBMinConns, cfg.Activity.DBMaxConns, "mcmods-cn-activity")
}

// ConnectLoadObserver keeps the sequential sampler to one connection, included in its counts.
func ConnectLoadObserver(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	return connectPool(ctx, cfg.DB, 0, 1, "mcmods-load-observer")
}

func connectPool(ctx context.Context, dbConfig config.DBConfig, minConns, maxConns int32, applicationName string) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(dbConfig.ConnString())
	if err != nil {
		return nil, RedactedError("parse database configuration", err)
	}
	poolConfig.MaxConns = maxConns
	poolConfig.MinConns = minConns
	poolConfig.MaxConnIdleTime = 5 * time.Minute
	poolConfig.MaxConnLifetime = 30 * time.Minute
	poolConfig.HealthCheckPeriod = 30 * time.Second
	poolConfig.ConnConfig.ConnectTimeout = 10 * time.Second
	poolConfig.ConnConfig.RuntimeParams["application_name"] = applicationName
	// Date-valued counters and projections use the PostgreSQL session day.
	// Override even an explicit DATABASE_URL setting so every application pool
	// assigns the same event instant to the same UTC day bucket.
	poolConfig.ConnConfig.RuntimeParams["timezone"] = "UTC"
	// Writes re-read current state after waiting on row locks. Callers that
	// need a stable snapshot select their isolation level with BeginTx.
	poolConfig.ConnConfig.RuntimeParams["default_transaction_isolation"] = "read committed"
	poolConfig.ConnConfig.RuntimeParams["search_path"] = applicationSearchPath
	poolConfig.ConnConfig.RuntimeParams["lock_timeout"] = "10s"
	poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = "5min"
	poolConfig.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "60s"
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	poolConfig.ConnConfig.DialFunc = dialer.DialContext

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, RedactedError("initialize database pool", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, RedactedError("connect to database", err)
	}
	return pool, nil
}

// RedactedError exposes a caller-supplied stage and a bounded error classification.
// The original driver error remains available to errors.Is and errors.As.
func RedactedError(stage string, cause error) error {
	if cause == nil {
		return nil
	}
	return &diagnosticError{stage: stage, cause: cause}
}

type diagnosticError struct {
	stage string
	cause error
}

func (err *diagnosticError) Error() string {
	if errors.Is(err.cause, context.Canceled) {
		return err.stage + ": context canceled"
	}
	if errors.Is(err.cause, context.DeadlineExceeded) {
		return err.stage + ": deadline exceeded"
	}
	var postgresError *pgconn.PgError
	if errors.As(err.cause, &postgresError) && validSQLState(postgresError.Code) {
		return err.stage + ": PostgreSQL SQLSTATE " + postgresError.Code
	}
	return fmt.Sprintf("%s: %T", err.stage, err.cause)
}

func (err *diagnosticError) Unwrap() error { return err.cause }

func validSQLState(code string) bool {
	if len(code) != 5 {
		return false
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			if character < 'A' || character > 'Z' {
				return false
			}
		}
	}
	return true
}
