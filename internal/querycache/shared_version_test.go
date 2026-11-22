package querycache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"mcmods-cn-backend/internal/config"
)

func TestPublishedSecurityVersionDoesNotMoveBackwards(t *testing.T) {
	redisServer := miniredis.RunT(t)
	cfg := config.RedisConfig{Enabled: true, Addr: redisServer.Addr(), Prefix: "mcmods", Namespace: "two-publishers", PoolSize: 2, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second}
	first, second := New(cfg), New(cfg)
	defer first.Close()
	defer second.Close()
	const key = "versions:rbac"
	ctx := context.Background()
	// Consecutive PostgreSQL bigints above the exact range of a Lua double.
	const latest int64 = 9007199254740993
	for _, write := range []struct {
		cache   *Cache
		version int64
	}{{first, latest - 2}, {second, latest}, {first, latest - 1}, {second, latest}} {
		if err := write.cache.PublishSecurityVersion(ctx, key, write.version, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := redisServer.Get("mcmods:two-publishers:" + key)
	if err != nil || raw != "9007199254740993" {
		t.Fatalf("published version=%q err=%v", raw, err)
	}
	redisServer.Close()
	if err := first.PublishSecurityVersion(ctx, key, latest, time.Minute); err == nil {
		t.Fatal("publication failure hidden")
	}
}

func TestReadingShortDenialMarkerDoesNotExtendItInAnotherProcess(t *testing.T) {
	store := miniredis.RunT(t)
	cfg := config.RedisConfig{Enabled: true, Addr: store.Addr(), Prefix: "test", Namespace: "denial-marker", TTL: time.Hour}
	a, b := New(cfg), New(cfg)
	defer a.Close()
	defer b.Close()
	ctx := t.Context()
	if !a.Set(ctx, "session-invalid:test", []byte("1"), time.Second) {
		t.Fatal("failed to write short marker")
	}
	if !b.HasMarker(ctx, "session-invalid:test") {
		t.Fatal("shared marker missing")
	}
	store.FastForward(2 * time.Second)
	if b.HasMarker(ctx, "session-invalid:test") {
		t.Fatal("reader extended an expired shared marker")
	}
}
