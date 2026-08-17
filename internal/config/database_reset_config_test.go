package config

import (
	"strings"
	"testing"
)

func TestDevelopmentResetRequiresExactDatabaseConfirmation(t *testing.T) {
	cfg := validActivityTestConfig()
	cfg.DB.Name = "mcmods_dev"
	cfg.DB.ResetOnStart = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), `RESET mcmods_dev`) {
		t.Fatalf("expected exact reset confirmation error, got %v", err)
	}
	cfg.DB.ResetConfirm = "RESET mcmods_dev"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid development reset was rejected: %v", err)
	}
}

func TestResetUsesDatabaseURLNameAndRejectsProductionNames(t *testing.T) {
	cfg := validActivityTestConfig()
	cfg.DB.Name = "ignored_dev"
	cfg.DB.URL = "postgres://local@127.0.0.1/mcmods_production?sslmode=disable"
	cfg.DB.ResetOnStart = true
	cfg.DB.ResetConfirm = "RESET mcmods_production"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "prod") {
		t.Fatalf("expected production-name reset refusal, got %v", err)
	}
}

func TestResetIsRejectedOutsideDevelopment(t *testing.T) {
	cfg := validActivityTestConfig()
	cfg.Env = "staging"
	cfg.DB.Name = "mcmods_dev"
	cfg.DB.ResetOnStart = true
	cfg.DB.ResetConfirm = "RESET mcmods_dev"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "only allowed in development") {
		t.Fatalf("expected environment reset refusal, got %v", err)
	}
}
