package querycache

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestLocalFallbackCachesAndInvalidates(t *testing.T) {
	cache := New(config.RedisConfig{TTL: time.Minute})
	var loads atomic.Int32
	load := func(context.Context) ([]byte, error) {
		loads.Add(1)
		return []byte(`{"ok":true}`), nil
	}
	for range 2 {
		value, err := cache.GetOrLoad(context.Background(), "global-tags:list", load)
		if err != nil || string(value) != `{"ok":true}` {
			t.Fatalf("unexpected cached value %q: %v", value, err)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("loader called %d times", loads.Load())
	}
	cache.InvalidatePrefix(context.Background(), "global-tags:")
	if _, err := cache.GetOrLoad(context.Background(), "global-tags:list", load); err != nil {
		t.Fatal(err)
	}
	if loads.Load() != 2 {
		t.Fatalf("loader was not called after invalidation: %d", loads.Load())
	}
}

func TestConsumeRateLimitLocalFallbackIsAtomic(t *testing.T) {
	t.Parallel()
	cache := New(config.RedisConfig{})
	const attempts, limit = 64, 11
	var allowed atomic.Int32
	var group sync.WaitGroup
	for range attempts {
		group.Add(1)
		go func() {
			defer group.Done()
			result := cache.ConsumeRateLimit(context.Background(), "comment:user:42", limit, time.Minute)
			if result.Allowed {
				allowed.Add(1)
			}
			if result.Backend != "local" || result.RetryAfter <= 0 {
				t.Errorf("unexpected fallback result: %+v", result)
			}
		}()
	}
	group.Wait()
	if got := allowed.Load(); got != limit {
		t.Fatalf("allowed %d concurrent requests, want exactly %d", got, limit)
	}
}

func TestLocalPresenceCountsRecentVisitors(t *testing.T) {
	t.Parallel()
	cache := New(config.RedisConfig{})
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	cache.TouchPresence(context.Background(), "visitor-a", now.Add(-time.Minute))
	cache.TouchPresence(context.Background(), "visitor-b", now.Add(-6*time.Minute))
	cache.TouchPresence(context.Background(), "visitor-a", now)
	if got := cache.OnlinePresenceCount(context.Background(), now); got != 1 {
		t.Fatalf("OnlinePresenceCount() = %d, want 1", got)
	}
}

func TestClaimThrottleLocalFallback(t *testing.T) {
	cache := New(config.RedisConfig{})
	if !cache.ClaimThrottle(context.Background(), "view:user:mod", time.Minute) {
		t.Fatal("first throttle claim should be accepted")
	}
	if cache.ClaimThrottle(context.Background(), "view:user:mod", time.Minute) {
		t.Fatal("duplicate throttle claim inside the window should be rejected")
	}
	if !cache.ClaimThrottle(context.Background(), "view:user:other-mod", time.Minute) {
		t.Fatal("a different throttle key should be accepted")
	}
}
