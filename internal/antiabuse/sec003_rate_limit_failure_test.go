package antiabuse

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestSEC003ExpensiveAnonymousReadFailsClosedWhenSharedRedisFails(t *testing.T) {
	redisServer := miniredis.RunT(t)
	cache := querycache.New(config.RedisConfig{
		Enabled: true, Addr: redisServer.Addr(), Prefix: "mcmods", Namespace: "sec003-expensive-read",
		PoolSize: 2, MinIdleConns: 0,
		DialTimeout: 50 * time.Millisecond, ReadTimeout: 50 * time.Millisecond, WriteTimeout: 50 * time.Millisecond,
		RateLimitFailClosed: true,
	})
	t.Cleanup(func() { _ = cache.Close() })
	service := New(context.Background(), config.AntiAbuseConfig{
		Enabled: true, IPHashSecret: "sec003-private-hash-secret-at-least-32-bytes",
	}, nil, cache)
	redisServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	allowed, retryAfter := service.ExpensiveReadLimit(ctx, "catalog-export", "198.51.100.42", 10, time.Hour)
	if allowed || retryAfter <= 0 {
		t.Fatalf("expensive anonymous read allowed=%t retryAfter=%s, want fail-closed rejection", allowed, retryAfter)
	}
}
