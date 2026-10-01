package querycache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"mcmods-cn-backend/internal/config"
)

// Seed the original shared limit and exercise the real Lua trimming path.
// This is a protocol/algorithm test, not a production Redis throughput claim.
func TestTEST022SharedVisitorSetTrimsRealLuaStateAndExpires(t *testing.T) {
	server := miniredis.RunT(t)
	cache := New(config.RedisConfig{Enabled: true, Addr: server.Addr(), Prefix: "test022", Namespace: "cardinality"})
	t.Cleanup(func() {
		if err := cache.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	now := time.Now().UTC()
	entries := make([]redis.Z, maxSharedPresenceEntries+100)
	for index := range entries {
		entries[index] = redis.Z{Score: float64(now.Unix()), Member: fmt.Sprintf("visitor-%06d", index)}
	}
	if err := cache.redis.ZAdd(ctx, cache.prefix+"presence:online", entries...).Err(); err != nil {
		t.Fatal(err)
	}
	cache.TouchPresence(ctx, "last-admitted", now)
	if got := cache.OnlinePresenceCount(ctx, now); got != maxSharedPresenceEntries {
		t.Fatalf("shared cardinality=%d want=%d", got, maxSharedPresenceEntries)
	}
	if got := cache.OnlinePresenceCount(ctx, now.Add(5*time.Minute+time.Second)); got != 0 {
		t.Fatalf("expired shared visitors=%d", got)
	}
	server.Close()
	if got := cache.OnlinePresenceCount(ctx, now.Add(5*time.Minute+time.Second)); got != 0 {
		t.Fatalf("fallback resurrected expired visitors=%d", got)
	}
}

func TestTEST022ConcurrentLocalVisitorCardinalityAndExpiryAreBounded(t *testing.T) {
	cache := New(config.RedisConfig{})
	ctx := context.Background()
	now := time.Now().UTC()
	var group sync.WaitGroup
	for worker := range 16 {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for index := range 512 {
				cache.TouchPresence(ctx, fmt.Sprintf("visitor-%d-%d", worker, index), now)
			}
		}(worker)
	}
	group.Wait()
	cache.mu.Lock()
	got := len(cache.presence)
	cache.mu.Unlock()
	if got != maxLocalPresenceEntries || cache.OnlinePresenceCount(ctx, now) != maxLocalPresenceEntries {
		t.Fatalf("local hard cap=%d want=%d", got, maxLocalPresenceEntries)
	}
	if got := cache.OnlinePresenceCount(ctx, now.Add(5*time.Minute+time.Second)); got != 0 {
		t.Fatalf("expired local visitors=%d", got)
	}
}
