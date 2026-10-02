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
		// A persisted record is the complete authority. In particular, false
		// feature flags and empty credentials are intentional values rather than
		// signals to recover a differently-aged configuration from the process
		// environment.
		return decodeNATSConfig(raw)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return queue.NormalizeConfig(fallback), nil
	}
	return queue.NormalizeConfig(fallback), err
}

func decodeNATSConfig(raw []byte) (config.NATSConfig, error) {
	var stored config.NATSConfig
	if err := json.Unmarshal(raw, &stored); err != nil {
		return config.NATSConfig{}, err
	}
	return queue.NormalizeConfig(stored), nil
}
