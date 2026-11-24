package querycache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestUnreadDeltaKeepsGlobalInvalidation(t *testing.T) {
	cache, _ := redisStateTestCache(t)
	assertUnreadDeltaKeepsGlobalInvalidation(t, cache)
}

func TestUnreadDeltaKeepsGlobalInvalidationWithRealRedis(t *testing.T) {
	if os.Getenv("MCMODS_RUN_REDIS_INTEGRATION") != "1" {
		t.Skip("requires a dedicated loopback Redis and MCMODS_RUN_REDIS_INTEGRATION=1")
	}
	cfg := config.Load().Redis
	host, _, err := net.SplitHostPort(cfg.Addr)
	ip := net.ParseIP(host)
	if err != nil || !cfg.Enabled || ip == nil || !ip.IsLoopback() {
		t.Fatal("explicitly configure a dedicated loopback Redis for the integration test")
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	cfg.Namespace = "unread-epoch-" + hex.EncodeToString(nonce[:])
	cfg.UnreadCounterEnabled = true
	cache := New(cfg)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cache.redis.Del(ctx, cache.prefix+"unread:user:9", cache.prefix+"unread:user:42", cache.prefix+"unread:global-epoch").Err(); err != nil {
			t.Error(err)
		}
		if err := cache.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = cache.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	assertUnreadDeltaKeepsGlobalInvalidation(t, cache)
	assertUnreadSummaryFollowsPeerUpdates(t, cache)
}

func assertUnreadDeltaKeepsGlobalInvalidation(t *testing.T, cache *Cache) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	peer := New(cache.Config())
	t.Cleanup(func() {
		if err := peer.Close(); err != nil {
			t.Error(err)
		}
	})
	old := UnreadSummary{Notifications: 1, Messages: 2}
	if got, err := cache.LoadUnread(ctx, 9, func(context.Context) (UnreadSummary, error) { return old, nil }); err != nil || got != old {
		t.Fatalf("initial unread data=%+v err=%v", got, err)
	}
	peer.BumpUnreadEpoch(ctx)
	cache.AdjustUnread(ctx, 9, "messages", 1)

	expected := UnreadSummary{Notifications: 2, Messages: 3}
	loads := 0
	loadTruth := func(context.Context) (UnreadSummary, error) {
		loads++
		return expected, nil
	}
	got, err := peer.LoadUnread(ctx, 9, loadTruth)
	if err != nil || got != expected || loads != 1 {
		t.Fatalf("after broadcast and message update unread=%+v want=%+v loads=%d err=%v", got, expected, loads, err)
	}
	peer.AdjustUnread(ctx, 9, "messages", 1)
	expected.Messages++
	got, err = cache.LoadUnread(ctx, 9, loadTruth)
	if err != nil || got != expected || loads != 1 {
		t.Fatalf("current-version delta unread=%+v want=%+v loads=%d err=%v", got, expected, loads, err)
	}
}

func TestUnreadSummaryFollowsPeerUpdatesAndInvalidations(t *testing.T) {
	reader, _ := redisStateTestCache(t)
	assertUnreadSummaryFollowsPeerUpdates(t, reader)
}

func assertUnreadSummaryFollowsPeerUpdates(t *testing.T, reader *Cache) {
	t.Helper()
	writer := New(reader.Config())
	t.Cleanup(func() { _ = writer.Close() })
	ctx := t.Context()
	truth := UnreadSummary{Notifications: 2, Messages: 1}
	loads := 0
	load := func(context.Context) (UnreadSummary, error) {
		loads++
		return truth, nil
	}
	check := func(wantLoads int) {
		t.Helper()
		got, err := reader.LoadUnread(ctx, 42, load)
		if err != nil || got != truth || loads != wantLoads {
			t.Fatalf("summary=%+v want=%+v loads=%d want=%d err=%v", got, truth, loads, wantLoads, err)
		}
	}
	check(1)
	truth.Messages++
	writer.AdjustUnread(ctx, 42, "messages", 1)
	check(1)
	truth.Notifications = 0
	writer.InvalidateUnread(ctx, 42)
	check(2)
}
