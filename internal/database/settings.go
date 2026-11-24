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
	setting, err := ReadNATSConfig(ctx, db, fallback, settingsEncryptionKey)
	return setting.Config, err
}

type NATSConfigSetting struct {
	Config    config.NATSConfig
	Encrypted []byte
}

func ReadNATSConfig(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, fallback config.NATSConfig, settingsEncryptionKey string) (NATSConfigSetting, error) {
	var stored []byte
	err := db.QueryRow(ctx, `select value from system_settings where key = 'nats.config'`).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return NATSConfigSetting{Config: queue.NormalizeConfig(fallback)}, nil
	}
	if err != nil {
		return NATSConfigSetting{}, err
	}
	plaintext, err := security.DecryptSetting(settingsEncryptionKey, stored)
	if err != nil {
		return NATSConfigSetting{}, err
	}
	cfg, err := decodeNATSConfig(plaintext)
	if err != nil {
		return NATSConfigSetting{}, err
	}
	return NATSConfigSetting{Config: cfg, Encrypted: stored}, nil
}

func decodeNATSConfig(raw []byte) (config.NATSConfig, error) {
	var stored config.NATSConfig
	if err := json.Unmarshal(raw, &stored); err != nil {
		return config.NATSConfig{}, err
	}
	return queue.NormalizeConfig(stored), nil
}
