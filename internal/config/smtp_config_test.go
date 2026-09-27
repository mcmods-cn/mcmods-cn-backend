package config

import "testing"

func TestSMTPEnabledEnvironmentOverridesConfiguredHost(t *testing.T) {
	t.Setenv("SMTP_HOST", "smtp.example.test")
	t.Setenv("SMTP_FROM", "no-reply@example.test")
	t.Setenv("SMTP_ENABLED", "false")
	if cfg := Load().SMTP; cfg.Enabled {
		t.Fatalf("SMTP_ENABLED=false must override configured fields: %#v", cfg)
	}

	t.Setenv("SMTP_ENABLED", "true")
	if cfg := Load().SMTP; !cfg.Enabled {
		t.Fatalf("SMTP_ENABLED=true should enable a configured environment fallback: %#v", cfg)
	}
}
