package querycache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestAuditMSG001SharedLeaseDoesNotFallBackDuringRedisOutage(t *testing.T) {
	redisServer := miniredis.RunT(t)
	first := newSEC003SharedRateLimitCache(t, redisServer.Addr(), "audit-msg001", true)
	second := newSEC003SharedRateLimitCache(t, redisServer.Addr(), "audit-msg001", true)
	ctx := context.Background()
	lease, ok := first.AcquireRealtimeLease(ctx, 42, "session", 2, 1, 1, time.Minute)
	if !ok {
		t.Fatal("initial shared lease rejected")
	}
	if _, ok := second.AcquireRealtimeLease(ctx, 42, "session", 2, 1, 1, time.Minute); ok {
		t.Fatal("second instance bypassed shared user/session budget")
	}
	redisServer.Close()
	for _, instance := range []*Cache{first, second} {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, ok := instance.AcquireRealtimeLease(ctx, 42, "session", 2, 1, 1, time.Minute)
		cancel()
		if ok {
			t.Fatal("Redis outage granted a local lease outside the shared budget")
		}
	}
	if lease.Refresh(ctx) {
		t.Fatal("disconnected shared lease refreshed")
	}
	lease.Release(ctx)
}

func TestSharedRealtimeLeaseRefreshRequiresEveryBudgetToBeLive(t *testing.T) {
	for _, suffix := range []string{"all", "user:42", "session:session"} {
		t.Run(suffix, func(t *testing.T) {
			store := miniredis.RunT(t)
			cache := newSEC003SharedRateLimitCache(t, store.Addr(), "lease-refresh", true)
			ctx := t.Context()
			lease, ok := cache.AcquireRealtimeLease(ctx, 42, "session", 2, 1, 1, time.Minute)
			if !ok {
				t.Fatal("initial shared lease rejected")
			}
			defer lease.Release(ctx)
			if !lease.Refresh(ctx) {
				t.Fatal("active shared lease failed to renew")
			}
			// Redis keeps each sorted set after its individual lease entries expire.
			key := cache.prefix + "realtime:leases:" + suffix
			if _, err := store.ZAdd(key, float64(time.Now().Add(-time.Second).UnixMilli()), lease.ID()); err != nil {
				t.Fatal(err)
			}
			if lease.Refresh(ctx) {
				t.Fatal("late heartbeat renewed an expired lease")
			}
			if score, err := store.ZScore(key, lease.ID()); err != nil || score >= float64(time.Now().UnixMilli()) {
				t.Fatalf("failed renewal changed the expired deadline: score=%v err=%v", score, err)
			}
		})
	}
}
