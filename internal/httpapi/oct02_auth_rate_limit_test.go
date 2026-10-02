package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestOCT02AuthenticationOutageCountsEachAttemptOnce(t *testing.T) {
	redis := miniredis.RunT(t)
	cache := querycache.New(config.RedisConfig{Enabled: true, Addr: redis.Addr(), Prefix: "oct02", Namespace: "auth-limit",
		AuthRateLimitEnabled: true, DialTimeout: 20 * time.Millisecond, ReadTimeout: 20 * time.Millisecond, WriteTimeout: 20 * time.Millisecond})
	t.Cleanup(func() { _ = cache.Close() })
	redis.Close()
	s := &Server{cache: cache}
	for attempt := 1; attempt <= 4; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		limited := s.authenticationRateLimited(ctx, "fixture", authLimitDimension{"account", "synthetic", 12, 3, time.Minute})
		cancel()
		if limited != (attempt > 3) {
			t.Fatalf("outage attempt %d limited=%v, want %v", attempt, limited, attempt > 3)
		}
	}
}

func TestOCT02AuthenticationRedisAllowancesPrewarmOutageCounter(t *testing.T) {
	redis := miniredis.RunT(t)
	cache := querycache.New(config.RedisConfig{Enabled: true, Addr: redis.Addr(), Prefix: "oct02", Namespace: "auth-warm",
		AuthRateLimitEnabled: true, DialTimeout: 20 * time.Millisecond, ReadTimeout: 20 * time.Millisecond, WriteTimeout: 20 * time.Millisecond})
	t.Cleanup(func() { _ = cache.Close() })
	s := &Server{cache: cache}
	for attempt := 1; attempt <= 2; attempt++ {
		if s.authenticationRateLimited(context.Background(), "fixture", authLimitDimension{"account", "synthetic", 12, 3, time.Minute}) {
			t.Fatalf("healthy Redis rejected attempt %d", attempt)
		}
	}
	redis.Close()
	for attempt := 3; attempt <= 4; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		limited := s.authenticationRateLimited(ctx, "fixture", authLimitDimension{"account", "synthetic", 12, 3, time.Minute})
		cancel()
		if limited != (attempt > 3) {
			t.Fatalf("after Redis outage attempt %d limited=%v", attempt, limited)
		}
	}
}
