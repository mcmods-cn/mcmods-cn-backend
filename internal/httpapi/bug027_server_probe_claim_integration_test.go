package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestServerProbeClaimRollsBackWhenClaimRowsCannotBeReadIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify server probe claim rollback")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	config.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create temporary table minecraft_servers(
		id text primary key,address text not null,review_status text not null,next_probe_at timestamptz not null
	); insert into minecraft_servers values('not-an-int64','127.0.0.1:1','approved',clock_timestamp()-interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	var before time.Time
	if err = pool.QueryRow(ctx, `select next_probe_at from minecraft_servers where id='not-an-int64'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	probeDueMinecraftServers(ctx, pool)
	var after time.Time
	if err = pool.QueryRow(ctx, `select next_probe_at from minecraft_servers where id='not-an-int64'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !after.Equal(before) {
		t.Fatalf("failed claim advanced next_probe_at: before=%s after=%s", before, after)
	}
}
