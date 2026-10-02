package querycache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"mcmods-cn-backend/internal/config"
)

func TestThrottleLeaseDuplicateDoesNotLoseOriginalReleaseToken(t *testing.T) {
	server := miniredis.RunT(t)
	cache := New(config.RedisConfig{Enabled: true, Addr: server.Addr()})
	defer cache.Close()
	ctx := context.Background()
	lease, ok, err := cache.ClaimThrottleLease(ctx, "duplicate", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first claim=%v err=%v", ok, err)
	}
	if _, ok, err := cache.ClaimThrottleLease(ctx, "duplicate", time.Minute); err != nil || ok {
		t.Fatalf("duplicate claim=%v err=%v", ok, err)
	}
	if err := lease.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cache.ClaimThrottleLease(ctx, "duplicate", time.Minute); err != nil || !ok {
		t.Fatalf("release did not permit retry claim=%v err=%v", ok, err)
	}
}

func TestThrottleLeaseLateReleaseCannotDeleteNewSharedOrLocalOwner(t *testing.T) {
	server := miniredis.RunT(t)
	cache := New(config.RedisConfig{Enabled: true, Addr: server.Addr()})
	defer cache.Close()
	ctx := context.Background()
	old, ok, err := cache.ClaimThrottleLease(ctx, "same-key", time.Minute)
	if err != nil || !ok {
		t.Fatal(err)
	}
	server.FastForward(time.Minute)
	newer, ok, err := cache.ClaimThrottleLease(ctx, "same-key", time.Minute)
	if err != nil || !ok {
		t.Fatalf("new owner claim=%v err=%v", ok, err)
	}
	if err := old.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cache.ClaimThrottleLease(ctx, "same-key", time.Minute); err != nil || ok {
		t.Fatalf("old release deleted new owner claim=%v err=%v", ok, err)
	}
	cache.mu.Lock()
	token := cache.claimOwners["same-key"]
	cache.mu.Unlock()
	if token != newer.token {
		t.Fatal("old release removed local owner")
	}
	if err := newer.Release(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestThrottleLeaseLocalExpiryAndRedisFailureRemainOwnerSafe(t *testing.T) {
	cache := New(config.RedisConfig{})
	defer cache.Close()
	ctx := context.Background()
	old, ok, err := cache.ClaimThrottleLease(ctx, "local", time.Minute)
	if err != nil || !ok {
		t.Fatal(err)
	}
	cache.mu.Lock()
	cache.claims["local"] = time.Now().Add(-time.Second)
	cache.mu.Unlock()
	newer, ok, err := cache.ClaimThrottleLease(ctx, "local", time.Minute)
	if err != nil || !ok {
		t.Fatalf("new local owner claim=%v err=%v", ok, err)
	}
	if err := old.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cache.ClaimThrottleLease(ctx, "local", time.Minute); err != nil || ok {
		t.Fatalf("stale local release cleared newer owner claim=%v err=%v", ok, err)
	}
	if err := newer.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cache.ClaimThrottleLease(ctx, "local", time.Minute); err != nil || !ok {
		t.Fatalf("local retry claim=%v err=%v", ok, err)
	}

	server := miniredis.RunT(t)
	shared := New(config.RedisConfig{Enabled: true, Addr: server.Addr(), DialTimeout: 20 * time.Millisecond, ReadTimeout: 20 * time.Millisecond, WriteTimeout: 20 * time.Millisecond})
	defer shared.Close()
	lease, ok, err := shared.ClaimThrottleLease(ctx, "outage", time.Minute)
	if err != nil || !ok {
		t.Fatal(err)
	}
	server.Close()
	deadline, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := lease.Release(deadline); err == nil {
		t.Fatal("shared release failure was hidden")
	}
	if _, ok, err := shared.ClaimThrottleLease(deadline, "outage", time.Minute); err != nil || !ok {
		t.Fatalf("failed attempt retained local claim during outage claim=%v err=%v", ok, err)
	}
}
