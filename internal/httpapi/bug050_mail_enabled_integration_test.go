package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestBUG050PersistedMailDisabledControlsEveryRuntimeMailerIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify the runtime mail enabled switch")
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	db, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	ctx := context.Background()
	if _, err = db.Exec(ctx, `create temp table system_settings(key text primary key, value jsonb not null)`); err != nil {
		t.Fatal(err)
	}

	cfg := config.Load()
	cfg.SMTP = config.SMTPConfig{
		Enabled: true, Host: "smtp.example.test", Port: 587, Username: "mailer", Password: "secret",
		From: "MCMods <no-reply@example.test>", UseTLS: true,
	}
	server := &Server{db: db, cfg: cfg}
	payload := mailConfigPayload{
		Enabled: false, Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, Username: cfg.SMTP.Username,
		Password: cfg.SMTP.Password, From: cfg.SMTP.From, UseTLS: cfg.SMTP.UseTLS,
	}
	sealed, err := server.sealSystemSetting(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `insert into system_settings(key,value) values ('mail.smtp',$1::jsonb)`, sealed); err != nil {
		t.Fatal(err)
	}

	if loaded, err := server.mailConfigFromSettings(ctx); err != nil || loaded.Enabled {
		t.Errorf("persisted enabled=false was recomputed to true: %#v", loaded)
	}
	if active, err := server.activeMailer(ctx); err != nil || active.Enabled() {
		t.Errorf("server runtime mailer ignored persisted enabled=false: %#v", active.Config)
	}
	worker := NewNotificationWorker(db, nil, nil, cfg.SMTP, cfg.SettingsEncryptionKey)
	if active, err := worker.activeMailer(ctx); err != nil || active.Enabled() {
		t.Errorf("notification worker mailer ignored persisted enabled=false: %#v", active.Config)
	}
}
