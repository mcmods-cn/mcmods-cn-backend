package querycache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestAuditMSG001SharedLeaseDoesNotFallBackDuringRedisOutage(t *testing.T) {
	redisServer := miniredis.RunT(t)
	first := newSEC003SharedRateLimitCache(t, redisServer.Addr(), "audit-msg001", false)
	second := newSEC003SharedRateLimitCache(t, redisServer.Addr(), "audit-msg001", false)
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
