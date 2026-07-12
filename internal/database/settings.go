package database

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

func LoadNATSConfig(ctx context.Context, db *pgxpool.Pool, fallback config.NATSConfig) (config.NATSConfig, error) {
	var raw []byte
	err := db.QueryRow(ctx, `select value from system_settings where key = 'nats.config'`).Scan(&raw)
	if err == nil {
		var stored config.NATSConfig
		if err := json.Unmarshal(raw, &stored); err != nil {
			return queue.NormalizeConfig(fallback), err
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
	return stored
}
