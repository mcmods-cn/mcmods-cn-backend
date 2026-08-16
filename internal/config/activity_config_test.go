package config

import (
	"strings"
	"testing"
	"time"
)

func validActivityTestConfig() Config {
	return Config{
		Env: "development", ReplicaCount: 1, FrontendOrigin: "http://localhost:3000",
		JWTSecret: "development", SettingsEncryptionKey: "01234567890123456789012345678901", JWTTTL: time.Hour,
		DB: DBConfig{MinConns: 1, MaxConns: 12},
		Activity: ActivityConfig{
			BatchSize: 256, QueueCapacity: 4096, FlushInterval: time.Second,
			RetryMinDelay: 250 * time.Millisecond, RetryMaxDelay: 30 * time.Second,
			WriteTimeout: 10 * time.Second, DurableEnqueueTimeout: time.Second,
			DBMinConns: 1, DBMaxConns: 4,
		},
	}
}

func TestMultipleReplicasRequireSharedRedisThrottle(t *testing.T) {
	cfg := validActivityTestConfig()
	cfg.ReplicaCount = 2
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "REDIS_ENABLED") {
		t.Fatalf("expected Redis validation error, got %v", err)
	}
	cfg.Redis.Enabled = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("shared Redis should make a multi-replica config valid: %v", err)
	}
}

func TestActivityQueueMustHoldAtLeastOneBatch(t *testing.T) {
	cfg := validActivityTestConfig()
	cfg.Activity.QueueCapacity = cfg.Activity.BatchSize - 1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "ACTIVITY_QUEUE_CAPACITY") {
		t.Fatalf("expected bounded queue validation error, got %v", err)
	}
}

func TestProductionRejectsDevelopmentAntiAbuseSecrets(t *testing.T) {
	cfg := validActivityTestConfig()
	cfg.Env = "production"
	cfg.FrontendOrigin = "https://mcmods.example"
	cfg.JWTSecret = "production-jwt-secret-that-is-at-least-32-bytes"
	cfg.AntiAbuse = AntiAbuseConfig{
		Enabled: true, HMACSecret: defaultDevelopmentAntiAbuseHMACSecret, IPHashSecret: defaultDevelopmentAntiAbuseIPSecret,
		ChallengeProvider: "proof", FormTokenTTL: time.Minute, ChallengeTTL: time.Minute,
		EventRetentionDays: 90, FingerprintRetentionDays: 14, DNSLookupTimeout: time.Second, DNSCacheTTL: time.Hour,
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "ANTI_ABUSE_HMAC_SECRET must be changed") || !strings.Contains(err.Error(), "ANTI_ABUSE_IP_HASH_SECRET must be changed") {
		t.Fatalf("expected production secret validation errors, got %v", err)
	}
}
