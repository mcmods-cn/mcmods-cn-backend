package database

import (
	"context"
	"net"
	"time"

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

func connectPool(ctx context.Context, dbConfig config.DBConfig, minConns, maxConns int32, applicationName string) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(dbConfig.ConnString())
	if err != nil {
		return nil, err
	}
	poolConfig.MaxConns = maxConns
	poolConfig.MinConns = minConns
	poolConfig.MaxConnIdleTime = 5 * time.Minute
	poolConfig.MaxConnLifetime = 30 * time.Minute
	poolConfig.HealthCheckPeriod = 30 * time.Second
	poolConfig.ConnConfig.ConnectTimeout = 10 * time.Second
	poolConfig.ConnConfig.RuntimeParams["application_name"] = applicationName
	poolConfig.ConnConfig.RuntimeParams["lock_timeout"] = "10s"
	poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = "5min"
	poolConfig.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "60s"
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	poolConfig.ConnConfig.DialFunc = dialer.DialContext

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
