package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestAuthenticationRateLimitConsumesOneAttemptPerBackend(t *testing.T) {
	for _, backend := range []string{"local-only", "redis-disabled", "redis", "redis-unavailable"} {
		t.Run(backend, func(t *testing.T) {
			cfg := config.RedisConfig{AuthRateLimitEnabled: backend != "local-only"}
			wantAllowed := 6
			if backend == "redis" || backend == "redis-unavailable" {
				redis := miniredis.RunT(t)
				cfg.Enabled, cfg.Addr = true, redis.Addr()
				cfg.Prefix, cfg.Namespace = "audit", backend
				cfg.DialTimeout, cfg.ReadTimeout, cfg.WriteTimeout = time.Millisecond*20, time.Millisecond*20, time.Millisecond*20
				if backend == "redis-unavailable" {
					redis.Close()
				} else {
					wantAllowed = 12
				}
			}
			cache := querycache.New(cfg)
			defer cache.Close()
			server := &Server{cache: cache}
			dimension := authLimitDimension{"account", "audit", 12, 6, 15 * time.Minute}
			for attempt := 1; attempt <= wantAllowed+1; attempt++ {
				limited := server.authenticationRateLimited(context.Background(), "login", dimension)
				if want := attempt > wantAllowed; limited != want {
					t.Fatalf("attempt %d limited=%v, want %v", attempt, limited, want)
				}
			}
		})
	}
}
