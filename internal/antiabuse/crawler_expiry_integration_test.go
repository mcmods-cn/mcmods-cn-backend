package antiabuse

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
)

func TestCachedCrawlerRuleFallsBackAtDatabaseDeadline(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires isolated PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg := config.Load()
	poolConfig, err := pgxpool.ParseConfig(cfg.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MinConns, poolConfig.MaxConns = 1, 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Error(err)
		}
	}()

	const ip = "192.0.2.1"
	var deadline time.Time
	// The permanent rule is older. Block precedence applies only until expiry.
	if _, err := pool.Exec(ctx, `insert into anti_abuse_bot_rules(kind,label,matcher) values('ip_allow','test',$1)`, ip); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into anti_abuse_bot_rules(kind,label,matcher,expires_at)
		values('ip_block','test',$1,clock_timestamp()+interval '4 seconds') returning expires_at`, ip).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	cache := querycache.New(config.RedisConfig{})
	defer cache.Close()
	service := &Service{cfg: config.AntiAbuseConfig{Enabled: true}, db: pool, cache: cache}
	if got, err := service.ClassifyCrawler(ctx, ip, "example", ""); err != nil || got != BlockedBot {
		t.Fatalf("temporary block got=%v err=%v", got, err)
	}
	if _, err := pool.Exec(ctx, "select pg_sleep_until($1)", deadline); err != nil {
		t.Fatal(err)
	}
	cached, err := service.botRules(ctx)
	if err != nil || len(cached) != 2 || cached[0].ExpiresAt == nil {
		t.Fatalf("expected both rules to remain in cache, length=%d err=%v", len(cached), err)
	}
	if got, err := service.ClassifyCrawler(ctx, ip, "example", ""); err != nil || got != AllowedBot {
		t.Fatalf("after expiry got=%v err=%v", got, err)
	}
}
