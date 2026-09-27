package mailer

import (
	"strings"
	"testing"

	"mcmods-cn-backend/internal/config"
)

func TestExplicitEnabledSwitchGatesConfiguredSMTP(t *testing.T) {
	cfg := config.SMTPConfig{
		Enabled: false, Host: "127.0.0.1", Port: 1, From: "no-reply@example.test",
	}
	disabled := New(cfg)
	if disabled.Enabled() {
		t.Fatal("configured SMTP must remain disabled when the explicit switch is false")
	}
	if err := disabled.Send("user@example.test", "subject", "body"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("disabled Send must stop before dialing SMTP: %v", err)
	}

	cfg.Enabled = true
	if !New(cfg).Enabled() {
		t.Fatal("valid SMTP configuration should be enabled when the explicit switch is true")
	}
	cfg.Host = ""
	if New(cfg).Enabled() {
		t.Fatal("the explicit switch cannot bypass required SMTP fields")
	}
}
