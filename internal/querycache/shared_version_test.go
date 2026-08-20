package querycache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"mcmods-cn-backend/internal/config"
)

func TestSharedVersionPointersAreVisibleAcrossCacheInstances(t *testing.T) {
	redisServer := miniredis.RunT(t)
	configuration := config.RedisConfig{
		Enabled: true, Addr: redisServer.Addr(), Prefix: "mcmods", Namespace: "shared-version-test",
		PoolSize: 2, MinIdleConns: 1, DialTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, TTL: time.Minute,
	}
	first := New(configuration)
	second := New(configuration)
	defer first.Close()
	defer second.Close()
	ctx := context.Background()
	key := UserPermissionVersionKey(42)
	if !first.SetShared(ctx, key, []byte("2"), time.Minute) {
		t.Fatal("failed to write shared permission version")
	}
	loaderCalled := false
	value, err := second.GetSharedOrLoadTTL(ctx, key, time.Minute, func(context.Context) ([]byte, error) {
		loaderCalled = true
		return []byte("unexpected"), nil
	})
	if err != nil || string(value) != "2" || loaderCalled {
		t.Fatalf("first shared read value=%q loader=%v err=%v", value, loaderCalled, err)
	}
	if !first.SetShared(ctx, key, []byte("3"), time.Minute) {
		t.Fatal("failed to update shared permission version")
	}
	value, err = second.GetSharedOrLoadTTL(ctx, key, time.Minute, func(context.Context) ([]byte, error) {
		return []byte("unexpected"), nil
	})
	if err != nil || string(value) != "3" {
		t.Fatalf("updated shared value=%q err=%v", value, err)
	}
}

func TestSharedVersionPointerFallsBackToAuthoritativeLoaderWithoutRedis(t *testing.T) {
	cache := New(config.RedisConfig{})
	defer cache.Close()
	loads := 0
	load := func(context.Context) ([]byte, error) {
		loads++
		return []byte("7"), nil
	}
	for range 2 {
		value, err := cache.GetSharedOrLoadTTL(context.Background(), UserPermissionVersionKey(42), time.Minute, load)
		if err != nil || string(value) != "7" {
			t.Fatalf("fallback value=%q err=%v", value, err)
		}
	}
	if loads != 2 {
		t.Fatalf("disabled Redis reused unverified process-local version: loads=%d", loads)
	}
}
