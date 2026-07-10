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
	if !errors.Is(err, pgx.ErrNoRows) {
		return queue.NormalizeConfig(fallback), err
	}

	legacy, legacyErr := loadLegacyAINATSConfig(ctx, db, fallback)
	if legacyErr != nil {
		return queue.NormalizeConfig(fallback), legacyErr
	}
	return queue.NormalizeConfig(legacy), nil
}

func loadLegacyAINATSConfig(ctx context.Context, db *pgxpool.Pool, fallback config.NATSConfig) (config.NATSConfig, error) {
	var raw []byte
	err := db.QueryRow(ctx, `select value from system_settings where key = 'ai.config'`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return fallback, nil
	}
	if err != nil {
		return fallback, err
	}
	var legacy struct {
		NATS struct {
			Enabled       bool   `json:"enabled"`
			URL           string `json:"url"`
			SubjectPrefix string `json:"subjectPrefix"`
			QueueGroup    string `json:"queueGroup"`
			MaxConcurrent int    `json:"maxConcurrent"`
		} `json:"nats"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return fallback, nil
	}
	if legacy.NATS.URL == "" && legacy.NATS.SubjectPrefix == "" {
		return fallback, nil
	}
	fallback.Enabled = legacy.NATS.Enabled
	fallback.URL = legacy.NATS.URL
	fallback.SubjectPrefix = legacy.NATS.SubjectPrefix
	if len(fallback.Tasks) == 0 {
		fallback.Tasks = queue.DefaultTaskConfigs()
	}
	for index := range fallback.Tasks {
		if fallback.Tasks[index].Code != "ai" {
			continue
		}
		if legacy.NATS.QueueGroup != "" {
			fallback.Tasks[index].QueueGroup = legacy.NATS.QueueGroup
		}
		if legacy.NATS.MaxConcurrent > 0 {
			fallback.Tasks[index].MaxConcurrent = legacy.NATS.MaxConcurrent
		}
	}
	return fallback, nil
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
