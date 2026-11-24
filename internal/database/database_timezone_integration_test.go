package database

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestApplicationDatabasePoolSessionDefaults(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify application database session defaults")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("invalid integration database URL")
	}
	query := parsed.Query()
	query.Set("timezone", "Pacific/Kiritimati")
	query.Set("default_transaction_isolation", "repeatable read")
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
		"observer":    ConnectLoadObserver,
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
			for _, test := range []struct {
				options pgx.TxOptions
				want    string
			}{
				{pgx.TxOptions{}, "read committed"},
				{pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, "repeatable read"},
			} {
				tx, err := pool.BeginTx(ctx, test.options)
				if err != nil {
					t.Fatal(err)
				}
				var actual string
				err = tx.QueryRow(ctx, "show transaction_isolation").Scan(&actual)
				endErr := tx.Rollback(ctx)
				if err != nil || endErr != nil {
					t.Fatalf("inspect transaction: %v; rollback: %v", err, endErr)
				}
				if actual != test.want {
					t.Errorf("transaction isolation = %q, want %q", actual, test.want)
				}
			}
		})
	}
}
