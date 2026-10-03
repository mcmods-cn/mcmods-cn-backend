package querycache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"mcmods-cn-backend/internal/config"
)

func newOCT03OwnedPresenceCache(t *testing.T) *Cache {
	t.Helper()
	if os.Getenv("MCMODS_RUN_REDIS_INTEGRATION") != "1" {
		t.Skip("requires an explicitly owned Redis server and MCMODS_RUN_REDIS_INTEGRATION=1")
	}
	cfg := config.Load().Redis
	host, _, err := net.SplitHostPort(cfg.Addr)
	if err != nil || !cfg.Enabled || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		t.Fatal("requires an explicitly configured loopback Redis server")
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	cfg.Namespace = "oct03-presence-" + hex.EncodeToString(nonce[:])
	cfg.PoolSize, cfg.MinIdleConns = 8, 0
	cache := New(cfg)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Delete only the two keys created by this test's random namespace.
		cleanup := redis.NewClient(&redis.Options{Addr: cfg.Addr, Username: cfg.Username, Password: cfg.Password, DB: cfg.DB})
		defer cleanup.Close()
		if err := cleanup.Del(ctx, cache.prefix+"presence:online", cache.prefix+"limit:presence:owned-source").Err(); err != nil {
			t.Error("clean up owned presence keys:", err)
		}
		_ = cache.Close()
	})
	if err := cache.Ping(context.Background()); err != nil {
		t.Fatal("owned Redis health check failed:", err)
	}
	return cache
}

func TestOCT03PresenceRealRedisAtomicCardinalityPruningAndFallback(t *testing.T) {
	cache := newOCT03OwnedPresenceCache(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	now := time.Now().UTC()
	key := cache.prefix + "presence:online"
	entries := make([]redis.Z, maxSharedPresenceEntries+100)
	for index := range entries {
		score := now.Unix()
		if index >= maxSharedPresenceEntries {
			score = now.Add(-6 * time.Minute).Unix()
		}
		entries[index] = redis.Z{Score: float64(score), Member: fmt.Sprintf("seed-%06d", index)}
	}
	if err := cache.redis.ZAdd(ctx, key, entries...).Err(); err != nil {
		t.Fatal(err)
	}
	cache.TouchPresence(ctx, "initial-newest", now.Add(time.Second))
	if count, err := cache.redis.ZCard(ctx, key).Result(); err != nil || count != 50_000 {
		t.Fatalf("initial shared cardinality=%d err=%v, want literal 50000", count, err)
	}
	if old, err := cache.redis.ZCount(ctx, key, "-inf", fmt.Sprint(now.Add(-5*time.Minute).Unix())).Result(); err != nil || old != 0 {
		t.Fatalf("expired members retained=%d err=%v", old, err)
	}
	var group sync.WaitGroup
	var oversized atomic.Bool
	for worker := range 8 {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for index := range 32 {
				cache.TouchPresence(ctx, fmt.Sprintf("new-%02d-%02d", worker, index), now.Add(2*time.Second))
			}
		}(worker)
	}
	group.Add(1)
	go func() {
		defer group.Done()
		for range 512 {
			count, err := cache.redis.ZCard(ctx, key).Result()
			if err != nil {
				t.Error(err)
				return
			}
			if count > 50_000 {
				oversized.Store(true)
			}
		}
	}()
	group.Wait()
	if oversized.Load() || cache.Metrics().Errors != 0 {
		t.Fatalf("concurrent shared trimming exceeded capacity=%v RedisErrors=%d", oversized.Load(), cache.Metrics().Errors)
	}
	if count, err := cache.redis.ZCard(ctx, key).Result(); err != nil || count != 50_000 {
		t.Fatalf("final shared cardinality=%d err=%v", count, err)
	}
	if _, err := cache.redis.ZScore(ctx, key, "new-07-31").Result(); err != nil {
		t.Fatal("newest admitted visitor was removed:", err)
	}
	if ttl, err := cache.redis.TTL(ctx, key).Result(); err != nil || ttl <= 0 || ttl > 10*time.Minute {
		t.Fatalf("shared presence TTL=%s err=%v", ttl, err)
	}
	if count := cache.OnlinePresenceCount(ctx, now.Add(6*time.Minute)); count != 0 {
		t.Fatalf("expired shared visitor count=%d", count)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	if count := cache.OnlinePresenceCount(ctx, now.Add(6*time.Minute)); count != 0 {
		t.Fatalf("Redis failure resurrected expired local visitors=%d", count)
	}
}

func TestOCT03PresenceRealRedisSharesSixtyAdmissionsAndKeepsOutageBudget(t *testing.T) {
	first := newOCT03OwnedPresenceCache(t)
	second := New(first.Config())
	t.Cleanup(func() { _ = second.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var allowed atomic.Int32
	var group sync.WaitGroup
	for index := range 96 {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			cache := first
			if index%2 == 1 {
				cache = second
			}
			result := cache.ConsumeRateLimitPolicy(ctx, "presence:owned-source", 60, 12, 5*time.Minute)
			if result.Backend != "redis" || result.RetryAfter <= 0 {
				t.Errorf("healthy shared decision=%+v", result)
			}
			if result.Allowed {
				allowed.Add(1)
			}
		}(index)
	}
	group.Wait()
	if allowed.Load() != 60 {
		t.Fatalf("shared concurrent admissions=%d want literal 60", allowed.Load())
	}
	for _, cache := range []*Cache{first, second} {
		if err := cache.Close(); err != nil {
			t.Fatal(err)
		}
		decision := cache.ConsumeRateLimitPolicy(ctx, "presence:owned-source", 60, 12, 5*time.Minute)
		if decision.Allowed || decision.Backend != "local" || decision.RetryAfter <= 0 {
			t.Fatalf("outage reset the warmed fallback budget: %+v", decision)
		}
		cache.cfg.RateLimitFailClosed = true
		decision = cache.ConsumeRateLimitPolicy(ctx, "presence:unseen-source", 60, 12, 5*time.Minute)
		if decision.Allowed || decision.Backend != "unavailable" {
			t.Fatalf("fail-closed replica allowed an outage request: %+v", decision)
		}
	}
}

func TestOCT03PresenceLocalFallbackKeepsLiteral4096CapacityAndExpiry(t *testing.T) {
	cache := New(config.RedisConfig{})
	ctx := context.Background()
	now := time.Now().UTC()
	var group sync.WaitGroup
	for worker := range 8 {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for index := range 1024 {
				cache.TouchPresence(ctx, fmt.Sprintf("local-%02d-%04d", worker, index), now)
			}
		}(worker)
	}
	group.Wait()
	if count := cache.OnlinePresenceCount(ctx, now); count != 4096 {
		t.Fatalf("local fallback capacity=%d want literal 4096", count)
	}
	cache.TouchPresence(ctx, "newest-after-capacity", now.Add(time.Second))
	cache.mu.Lock()
	_, retained := cache.presence["newest-after-capacity"]
	count := len(cache.presence)
	cache.mu.Unlock()
	if !retained || count != 4096 {
		t.Fatalf("latest visitor admission retained=%v cardinality=%d", retained, count)
	}
	if count := cache.OnlinePresenceCount(ctx, now.Add(5*time.Minute+2*time.Second)); count != 0 {
		t.Fatalf("expired local visitor count=%d", count)
	}
}
