package database

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestApplicationDatabasePoolsForceUTCSessionTimezone(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify database session timezone")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse integration database URL: %v", err)
	}
	query := parsed.Query()
	query.Set("timezone", "Pacific/Kiritimati")
	parsed.RawQuery = query.Encode()

	cfg := config.Config{
		DB:       config.DBConfig{URL: parsed.String(), MinConns: 0, MaxConns: 1},
		Activity: config.ActivityConfig{DBMinConns: 0, DBMaxConns: 1},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for name, connect := range map[string]func(context.Context, config.Config) (*pgxpool.Pool, error){
		"application": Connect,
		"activity":    ConnectActivity,
	} {
		t.Run(name, func(t *testing.T) {
			pool, connectErr := connect(ctx, cfg)
			if connectErr != nil {
				t.Fatalf("connect %s pool: %v", name, connectErr)
			}
			defer pool.Close()
			var timezone, fixedInstantDate string
			if queryErr := pool.QueryRow(ctx, `select current_setting('TimeZone'),
				('2026-08-23 23:30:00+00'::timestamptz)::date::text`).Scan(&timezone, &fixedInstantDate); queryErr != nil {
				t.Fatalf("inspect %s session: %v", name, queryErr)
			}
			if timezone != "UTC" || fixedInstantDate != "2026-08-23" {
				t.Fatalf("%s session timezone=%q fixedInstantDate=%q; want UTC/2026-08-23", name, timezone, fixedInstantDate)
			}
		})
	}
}
