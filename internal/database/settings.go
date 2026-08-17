package database

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

func LoadNATSConfig(ctx context.Context, db *pgxpool.Pool, fallback config.NATSConfig, settingsEncryptionKey string) (config.NATSConfig, error) {
	var raw []byte
	err := db.QueryRow(ctx, `select value from system_settings where key = 'nats.config'`).Scan(&raw)
	if err == nil {
		raw, err = security.DecryptSetting(settingsEncryptionKey, raw)
		if err != nil {
			return queue.NormalizeConfig(fallback), err
		}
		var stored config.NATSConfig
		if err := json.Unmarshal(raw, &stored); err != nil {
			return queue.NormalizeConfig(fallback), err
		}
		var present map[string]json.RawMessage
		_ = json.Unmarshal(raw, &present)
		if _, ok := present["outboxEnabled"]; !ok {
			stored.OutboxEnabled = fallback.OutboxEnabled
		}
		if _, ok := present["realtime"]; !ok {
			stored.Realtime = fallback.Realtime
		}
		if _, ok := present["jetStream"]; !ok {
			stored.JetStream = fallback.JetStream
		}
		return queue.NormalizeConfig(mergeNATSConfig(stored, fallback)), nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return queue.NormalizeConfig(fallback), nil
	}
	return queue.NormalizeConfig(fallback), err
}

func mergeNATSConfig(stored config.NATSConfig, fallback config.NATSConfig) config.NATSConfig {
	if stored.Password == "" {
		stored.Password = fallback.Password
	}
	if stored.Token == "" {
		stored.Token = fallback.Token
	}
	if stored.Tasks == nil {
		stored.Tasks = fallback.Tasks
	}
	if stored.JetStream.Stream == "" {
		stored.JetStream.Stream = fallback.JetStream.Stream
	}
	if stored.JetStream.MaxDeliver <= 0 {
		stored.JetStream.MaxDeliver = fallback.JetStream.MaxDeliver
	}
	if stored.JetStream.AckWait <= 0 {
		stored.JetStream.AckWait = fallback.JetStream.AckWait
	}
	if stored.JetStream.PublishTimeout <= 0 {
		stored.JetStream.PublishTimeout = fallback.JetStream.PublishTimeout
	}
	return stored
}
