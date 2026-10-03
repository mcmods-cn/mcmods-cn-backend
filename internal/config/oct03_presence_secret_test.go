package config

import (
	"strings"
	"testing"
)

func TestOCT03PublicPresenceRequiresProductionSecretWhenAntiAbuseIsDisabled(t *testing.T) {
	for _, secret := range []string{"", "short", defaultDevelopmentAntiAbuseHMACSecret} {
		t.Run(map[string]string{"": "empty", "short": "short", defaultDevelopmentAntiAbuseHMACSecret: "development-default"}[secret], func(t *testing.T) {
			cfg := validActivityTestConfig()
			cfg.Env = "production"
			cfg.FrontendOrigin = "https://mcmods.example"
			cfg.JWTSecret = "oct03-production-test-jwt-secret-at-least-32-bytes"
			cfg.AntiAbuse.Enabled = false
			cfg.AntiAbuse.HMACSecret = secret
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "ANTI_ABUSE_HMAC_SECRET") {
				t.Fatal("public presence accepted an absent, short, or development signing secret with anti-abuse disabled")
			}
		})
	}
	cfg := validActivityTestConfig()
	cfg.Env = "production"
	cfg.FrontendOrigin = "https://mcmods.example"
	cfg.JWTSecret = "oct03-production-test-jwt-secret-at-least-32-bytes"
	cfg.AntiAbuse.Enabled = false
	cfg.AntiAbuse.HMACSecret = "oct03-explicit-presence-signing-secret-at-least-32-bytes"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a valid presence secret required unrelated disabled anti-abuse fields: %v", err)
	}
}
