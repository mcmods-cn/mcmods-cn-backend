package config

import (
	"strings"
	"testing"
	"time"
)

func TestSEC003MultipleReplicasRequireStartupRedisAndFailClosedRateLimits(t *testing.T) {
	cfg := validActivityTestConfig()
	cfg.ReplicaCount = 2
	cfg.Redis = RedisConfig{
		Enabled: true, Addr: "127.0.0.1:6379", Namespace: "sec003",
		PoolSize: 4, MinIdleConns: 1,
		DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
		AuthRateLimitEnabled: true,
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "REDIS_REQUIRED") || !strings.Contains(err.Error(), "REDIS_RATE_LIMIT_FAIL_CLOSED") {
		t.Fatalf("multi-replica validation error = %v, want both startup and runtime failure contracts", err)
	}
	cfg.Redis.Required = true
	cfg.Redis.RateLimitFailClosed = true
	if err = cfg.Validate(); err != nil {
		t.Fatalf("valid multi-replica Redis failure policy rejected: %v", err)
	}
}

func TestSEC003FailClosedPolicyRequiresRedis(t *testing.T) {
	cfg := validActivityTestConfig()
	cfg.Redis.RateLimitFailClosed = true
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "REDIS_ENABLED must be true when REDIS_RATE_LIMIT_FAIL_CLOSED=true") {
		t.Fatalf("fail-closed policy without Redis validation error = %v", err)
	}
}
