package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/mailer"
	"mcmods-cn-backend/internal/security"
)

var errMailSettingsUnavailable = errors.New("mail settings are unavailable")

type mailSettingsReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readMailSettings(ctx context.Context, db mailSettingsReader, fallback config.SMTPConfig, key string) (mailConfigPayload, error) {
	var raw []byte
	err := db.QueryRow(ctx, `select value from system_settings where key='mail.smtp'`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return mailConfigPayload{Enabled: fallback.Enabled, Host: fallback.Host, Port: fallback.Port, Username: fallback.Username, Password: fallback.Password, From: fallback.From, UseTLS: fallback.UseTLS}, nil
	}
	if err != nil {
		return mailConfigPayload{}, fmt.Errorf("%w: %w", errMailSettingsUnavailable, err)
	}
	raw, err = security.DecryptSetting(key, raw)
	if err != nil {
		return mailConfigPayload{}, fmt.Errorf("%w: %w", errMailSettingsUnavailable, err)
	}
	// A stored value is authoritative, not an overlay on an enabled fallback.
	if trimmed := strings.TrimSpace(string(raw)); !strings.HasPrefix(trimmed, "{") {
		return mailConfigPayload{}, fmt.Errorf("%w: SMTP configuration must be an object", errMailSettingsUnavailable)
	}
	var payload mailConfigPayload
	if err = json.Unmarshal(raw, &payload); err != nil {
		return mailConfigPayload{}, fmt.Errorf("%w: %w", errMailSettingsUnavailable, err)
	}
	if payload.Enabled && !mailer.New(smtpConfigFromPayload(payload)).Enabled() {
		return mailConfigPayload{}, fmt.Errorf("%w: invalid enabled SMTP configuration", errMailSettingsUnavailable)
	}
	return payload, nil
}

func (s *Server) mailConfigFromSettings(ctx context.Context) (mailConfigPayload, error) {
	return readMailSettings(ctx, s.db, s.cfg.SMTP, s.cfg.SettingsEncryptionKey)
}

func (s *Server) activeMailer(ctx context.Context) (mailer.Mailer, error) {
	payload, err := s.mailConfigFromSettings(ctx)
	return mailer.New(smtpConfigFromPayload(payload)), err
}

func (s *Server) saveMailConfig(ctx context.Context, payload mailConfigPayload, actorID int64) (mailConfigPayload, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return mailConfigPayload{}, fmt.Errorf("%w: %w", errMailSettingsUnavailable, err)
	}
	defer tx.Rollback(ctx)
	// This also serializes the first insert and omitted-password saves across
	// replicas; no second in-memory SMTP configuration is maintained.
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('settings:mail.smtp',0))`); err != nil {
		return mailConfigPayload{}, fmt.Errorf("%w: %w", errMailSettingsUnavailable, err)
	}
	if strings.TrimSpace(payload.Password) == "" {
		current, err := readMailSettings(ctx, tx, s.cfg.SMTP, s.cfg.SettingsEncryptionKey)
		if err != nil {
			return mailConfigPayload{}, err
		}
		payload.Password = current.Password
	}
	raw, err := s.sealSystemSetting(payload)
	if err != nil {
		return mailConfigPayload{}, err
	}
	if _, err = tx.Exec(ctx, `insert into system_settings(key,value,updated_by,updated_at)
		values('mail.smtp',$1::jsonb,$2,now()) on conflict(key) do update
		set value=excluded.value,updated_by=excluded.updated_by,updated_at=now()`, raw, actorID); err != nil {
		return mailConfigPayload{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return mailConfigPayload{}, err
	}
	return payload, nil
}
