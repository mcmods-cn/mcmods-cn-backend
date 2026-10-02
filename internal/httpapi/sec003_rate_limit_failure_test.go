package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestSEC003AuthenticationFailsClosedWhenSharedRedisFails(t *testing.T) {
	redisServer := miniredis.RunT(t)
	cache := querycache.New(config.RedisConfig{
		Enabled: true, Addr: redisServer.Addr(), Prefix: "mcmods", Namespace: "sec003-auth",
		PoolSize: 2, MinIdleConns: 0,
		DialTimeout: 50 * time.Millisecond, ReadTimeout: 50 * time.Millisecond, WriteTimeout: 50 * time.Millisecond,
		RateLimitFailClosed: true,
		// A stale or manually assembled config must not make authentication
		// fall back locally when the multi-replica safety policy is active.
		AuthRateLimitEnabled: false,
	})
	t.Cleanup(func() { _ = cache.Close() })
	server := &Server{cache: cache}
	redisServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if !server.authenticationRateLimited(ctx, "login", authLimitDimension{
		name: "account", value: authRateDigest("user@example.test"), limit: 12, degradedLimit: 6, window: 15 * time.Minute,
	}) {
		t.Fatal("authentication was allowed to use a per-process quota during a shared Redis outage")
	}
}
