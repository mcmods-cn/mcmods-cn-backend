package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"mcmods-cn-backend/internal/antiabuse"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestAntiAbuseRetryAfterSecondsNeverEndsTheWindowEarly(t *testing.T) {
	for _, tc := range []struct {
		remaining time.Duration
		seconds   int
	}{
		{-time.Second, 0}, {0, 0}, {time.Nanosecond, 1},
		{time.Second, 1}, {time.Second + time.Nanosecond, 2},
		{59200 * time.Millisecond, 60},
	} {
		if got := antiAbuseRetryAfterSeconds(tc.remaining); got != tc.seconds {
			t.Errorf("remaining=%s advertised=%d want=%d", tc.remaining, got, tc.seconds)
		}
	}
}

func TestAuditREM004CrawlerRetryAfterCoversTheActualRemainingWindow(t *testing.T) {
	f := newTEST013Fixture(t)
	redis := miniredis.RunT(t)
	cache := querycache.New(config.RedisConfig{Enabled: true, Addr: redis.Addr(), Prefix: "audit-rem004", Namespace: "retry-seconds"})
	t.Cleanup(func() { _ = cache.Close() })
	ctx := f.ctx
	service := antiabuse.New(ctx, config.AntiAbuseConfig{Enabled: true, IPHashSecret: "audit-only-private-hash-secret-at-least-32-bytes"}, f.db, cache)
	t.Cleanup(func() { _ = service.Close(context.Background()) })
	const ip = "198.51.100.91"
	const path = "/api/v1/auth/me"
	// Seed the unchanged 60/minute unknown-crawler policy, then control only
	// the Redis clock. No production threshold/window or user-agent is changed.
	for i := 0; i < 60; i++ {
		if allowed, _ := service.ReadLimit(ctx, antiabuse.UnknownCrawler, ip, path); !allowed {
			t.Fatalf("unchanged budget rejected request %d", i+1)
		}
	}
	redis.FastForward(800 * time.Millisecond)
	server := &Server{antiAbuse: service, cache: cache, db: f.db}
	passed := 0
	handler := server.botTraffic(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { passed++; w.WriteHeader(204) }))
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = ip + ":32123"
		req.Header.Set("User-Agent", "AuditExampleSpider/1.0")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	response := request()
	if response.Code != 429 || passed != 0 {
		t.Fatalf("real middleware did not reject within its window: status=%d passed=%d", response.Code, passed)
	}
	seconds, err := strconv.Atoi(response.Header().Get("Retry-After"))
	if err != nil || seconds != 60 {
		t.Fatalf("59.2-second remaining window advertised %q; must round up to 60", response.Header().Get("Retry-After"))
	}
	redis.FastForward(time.Duration(seconds) * time.Second)
	response = request()
	if response.Code != 204 || passed != 1 {
		t.Fatalf("advertised wait did not restore actual middleware budget: status=%d passed=%d", response.Code, passed)
	}
}
