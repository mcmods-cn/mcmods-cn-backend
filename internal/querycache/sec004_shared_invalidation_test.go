package querycache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"mcmods-cn-backend/internal/config"
)

func TestSEC004DeleteSharedInvalidatesExactPointerAndReportsFailure(t *testing.T) {
	redisServer := miniredis.RunT(t)
	cache := New(config.RedisConfig{
		Enabled: true, Addr: redisServer.Addr(), Prefix: "mcmods", Namespace: "sec004",
		PoolSize: 2, MinIdleConns: 0,
		DialTimeout: 50 * time.Millisecond, ReadTimeout: 50 * time.Millisecond, WriteTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() { _ = cache.Close() })
	ctx := context.Background()
	key := UserPermissionVersionKey(42)
	if !cache.SetShared(ctx, key, []byte("7"), time.Minute) {
		t.Fatal("failed to seed shared version pointer")
	}
	if err := cache.DeleteShared(ctx, key); err != nil {
		t.Fatalf("delete shared pointer: %v", err)
	}
	if redisServer.Exists("mcmods:sec004:" + key) {
		t.Fatal("exact shared version pointer survived invalidation")
	}

	redisServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := cache.DeleteShared(ctx, key); err == nil {
		t.Fatal("Redis invalidation failure was silently reported as success")
	}
}
