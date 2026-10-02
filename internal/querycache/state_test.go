package querycache

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"mcmods-cn-backend/internal/config"
)

func redisStateTestCache(t *testing.T) (*Cache, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	cache := New(config.RedisConfig{
		Enabled: true, Addr: server.Addr(), Prefix: "mcmods", Namespace: "test",
		PoolSize: 4, MinIdleConns: 1, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
		TTL: time.Minute, PresenceEnabled: true, PresenceTTL: 2 * time.Minute,
		UnreadCounterEnabled: true, UnreadTTL: time.Minute,
	})
	t.Cleanup(func() { _ = cache.Close() })
	return cache, server
}

func TestPresenceTracksMultipleSessionsWithoutKeysScan(t *testing.T) {
	cache, _ := redisStateTestCache(t)
	ctx := context.Background()
	now := time.Now().UTC()
	cache.TouchUserPresence(ctx, 42, "session-a", now, 2*time.Minute)
	cache.TouchUserPresence(ctx, 42, "session-b", now, 2*time.Minute)
	cache.RemoveUserPresence(ctx, 42, "session-a", now, 2*time.Minute)
	if !cache.UsersOnline(ctx, []int64{42}, now, 2*time.Minute)[42] {
		t.Fatal("second session should keep user online")
	}
	cache.RemoveUserPresence(ctx, 42, "session-b", now, 2*time.Minute)
	if cache.UsersOnline(ctx, []int64{42}, now, 2*time.Minute)[42] {
		t.Fatal("user should be offline after all sessions leave")
	}
}

func TestUnreadCacheLoadsOnceAndNeverGoesNegative(t *testing.T) {
	cache, _ := redisStateTestCache(t)
	ctx := context.Background()
	var loads atomic.Int32
	loader := func(context.Context) (UnreadSummary, error) {
		loads.Add(1)
		return UnreadSummary{Notifications: 2, Messages: 1}, nil
	}
	first, err := cache.LoadUnread(ctx, 9, loader)
	if err != nil || first.Total() != 3 {
		t.Fatalf("unexpected unread value %+v err=%v", first, err)
	}
	cache.AdjustUnread(ctx, 9, "messages", -100)
	second, err := cache.LoadUnread(ctx, 9, loader)
	if err != nil || second.Messages != 0 || second.Notifications != 2 {
		t.Fatalf("unsafe unread decrement: %+v err=%v", second, err)
	}
	if loads.Load() != 1 {
		t.Fatalf("loader called %d times, want 1", loads.Load())
	}
}

func TestUnreadReconciliationRepairsDrift(t *testing.T) {
	cache, _ := redisStateTestCache(t)
	ctx := context.Background()
	_, err := cache.LoadUnread(ctx, 11, func(context.Context) (UnreadSummary, error) {
		return UnreadSummary{Notifications: 9, Messages: 4}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	drifted, err := cache.ReconcileUnread(ctx, 11, UnreadSummary{Notifications: 2, Messages: 1})
	if err != nil || !drifted {
		t.Fatalf("reconcile drifted=%v err=%v", drifted, err)
	}
	value, err := cache.LoadUnread(ctx, 11, func(context.Context) (UnreadSummary, error) {
		return UnreadSummary{}, nil
	})
	if err != nil || value.Notifications != 2 || value.Messages != 1 {
		t.Fatalf("unexpected reconciled value %+v err=%v", value, err)
	}
	metrics := cache.Metrics()
	if metrics.UnreadCalibrations != 1 || metrics.UnreadDrifts != 1 {
		t.Fatalf("unexpected reconciliation metrics %+v", metrics)
	}
}

func TestUnreadReconciliationCandidatesRotateOnlyLiveServedEntries(t *testing.T) {
	cache := New(config.RedisConfig{UnreadTTL: time.Minute})
	ctx := context.Background()
	for _, userID := range []int64{30, 10, 20} {
		if _, err := cache.LoadUnread(ctx, userID, func(context.Context) (UnreadSummary, error) {
			return UnreadSummary{Notifications: userID}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	cache.mu.Lock()
	expired := cache.unread[20]
	expired.expiresAt = time.Now().Add(-time.Second)
	cache.unread[20] = expired
	cache.mu.Unlock()

	first := cache.UnreadReconciliationCandidates(1)
	second := cache.UnreadReconciliationCandidates(1)
	if len(first) != 1 || len(second) != 1 || first[0] != 10 || second[0] != 30 {
		t.Fatalf("rotating candidates first=%v second=%v", first, second)
	}
	if third := cache.UnreadReconciliationCandidates(10); len(third) != 2 || third[0] != 10 || third[1] != 30 {
		t.Fatalf("wrapped candidates=%v", third)
	}
	cache.mu.Lock()
	_, retainedExpired := cache.unread[20]
	cache.mu.Unlock()
	if retainedExpired {
		t.Fatal("expired unread derivative remained a reconciliation candidate")
	}
}

func TestRedisKeysAreEnvironmentNamespaced(t *testing.T) {
	cache, redisServer := redisStateTestCache(t)
	cache.Set(context.Background(), "session:hashed", []byte("value"), time.Minute)
	if !redisServer.Exists("mcmods:test:session:hashed") {
		t.Fatalf("expected namespaced key, got %v", redisServer.Keys())
	}
}
