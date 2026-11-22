package config

import (
	"strings"
	"testing"
)

func TestProductionRejectsPublishedExampleSecrets(t *testing.T) {
	for _, environment := range []string{"production", "staging"} {
		t.Run(environment, func(t *testing.T) {
			cfg := validActivityTestConfig()
			cfg.Env = environment
			cfg.FrontendOrigin = "https://mcmods.example.test"
			cfg.JWTSecret = "synthetic-unique-jwt-signing-value-for-test"
			cfg.SettingsEncryptionKey = "synthetic-unique-settings-value-for-test"
			cfg.AntiAbuse.HMACSecret = "synthetic-unique-visitor-signing-value-for-test"
			if err := cfg.Validate(); err != nil {
				t.Fatalf("known valid fixture: %v", err)
			}
			// Previously published example values are public and cannot be credentials.
			cfg.JWTSecret = "change-this-to-a-long-random-string"
			cfg.SettingsEncryptionKey = "change-this-to-a-different-long-random-string"
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), "JWT_SECRET") || !strings.Contains(err.Error(), "SETTINGS_ENCRYPTION_KEY") {
				t.Fatal("startup accepted at least one published example secret")
			}
		})
	}
}
