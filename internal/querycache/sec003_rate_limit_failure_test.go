package querycache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"mcmods-cn-backend/internal/config"
)

func newSEC003SharedRateLimitCache(t *testing.T, address, namespace string, failClosed bool) *Cache {
	t.Helper()
	cache := New(config.RedisConfig{
		Enabled: true, Addr: address, Prefix: "mcmods", Namespace: namespace,
		PoolSize: 2, MinIdleConns: 0,
		DialTimeout: 50 * time.Millisecond, ReadTimeout: 50 * time.Millisecond, WriteTimeout: 50 * time.Millisecond,
		RateLimitFailClosed: failClosed,
	})
	t.Cleanup(func() { _ = cache.Close() })
	return cache
}

func TestSEC003SharedRateLimitsFailClosedAcrossReplicasWhenRedisFails(t *testing.T) {
	redisServer := miniredis.RunT(t)
	first := newSEC003SharedRateLimitCache(t, redisServer.Addr(), "sec003-shared", true)
	second := newSEC003SharedRateLimitCache(t, redisServer.Addr(), "sec003-shared", true)

	for index, cache := range []*Cache{first, second} {
		result := cache.ConsumeRateLimit(context.Background(), "login:network:example", 2, time.Minute)
		if !result.Allowed || result.Backend != "redis" {
			t.Fatalf("shared attempt %d = %+v, want Redis allowance", index+1, result)
		}
	}
	if result := first.ConsumeRateLimit(context.Background(), "login:network:example", 2, time.Minute); result.Allowed || result.Backend != "redis" {
		t.Fatalf("shared third attempt = %+v, want Redis rejection", result)
	}

	redisServer.Close()
	for index, cache := range []*Cache{first, second} {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		result := cache.ConsumeRateLimit(ctx, "expensive-export:user:42", 10, time.Hour)
		cancel()
		if result.Allowed || result.Backend != "unavailable" || result.RetryAfter <= 0 {
			t.Fatalf("replica %d outage result = %+v, want fail-closed rejection", index+1, result)
		}
		if got := cache.Metrics().RateLimitFailClosed; got != 1 {
			t.Fatalf("replica %d fail-closed metric = %d, want 1", index+1, got)
		}
	}
}

func TestSEC003SingleReplicaMayUseBoundedLocalFallback(t *testing.T) {
	redisServer := miniredis.RunT(t)
	cache := newSEC003SharedRateLimitCache(t, redisServer.Addr(), "sec003-local", false)
	redisServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	result := cache.ConsumeRateLimitPolicy(ctx, "login:account:example", 12, 3, time.Minute)
	if !result.Allowed || result.Backend != "local" || result.Remaining != 2 {
		t.Fatalf("single-replica outage result = %+v, want bounded local fallback", result)
	}
}
