package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestUnreadReconciliationQueryIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	redisServer := miniredis.RunT(t)
	cache := querycache.New(config.RedisConfig{
		Enabled: true, Addr: redisServer.Addr(), Prefix: "mcmods", Namespace: "unread-integration",
		PoolSize: 2, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
		UnreadCounterEnabled: true, UnreadTTL: time.Minute,
	})
	defer cache.Close()
	if _, _, _, err = reconcileUnreadBatch(ctx, pool, cache, 0, 5); err != nil {
		t.Fatal(err)
	}
}
