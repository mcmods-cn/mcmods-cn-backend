package database

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestLoadNATSConfigKeepsStoredFalseAndClearedSecretsIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify persisted NATS settings")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `set search_path=pg_temp,public`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `create temp table system_settings(key text primary key,value jsonb not null) on commit preserve rows`); err != nil {
		t.Fatal(err)
	}
	stored := config.NATSConfig{
		Enabled: false, URL: "nats://stored:4222", SubjectPrefix: "stored",
		Password: "", Token: "", OutboxEnabled: false, Realtime: false,
		Tasks:     []config.NATSTaskConfig{},
		JetStream: config.JetStreamConfig{Enabled: false, Stream: "STORED", MaxDeliver: 5, AckWait: 9 * time.Second, PublishTimeout: 2 * time.Second},
	}
	plaintext, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	const encryptionKey = "integration-test-settings-key-at-least-32-bytes"
	sealed, err := security.EncryptSetting(encryptionKey, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('nats.config',$1::jsonb)`, sealed); err != nil {
		t.Fatal(err)
	}
	fallback := config.NATSConfig{
		Enabled: true, Password: "environment-password", Token: "environment-token", OutboxEnabled: true, Realtime: true,
		JetStream: config.JetStreamConfig{Enabled: true, Stream: "ENV", MaxDeliver: 8, AckWait: time.Minute, PublishTimeout: 5 * time.Second},
	}
	loaded, err := LoadNATSConfig(ctx, pool, fallback, encryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Enabled || loaded.OutboxEnabled || loaded.Realtime || loaded.JetStream.Enabled || loaded.Password != "" || loaded.Token != "" {
		t.Fatalf("stored authority was overwritten by fallback: %#v", loaded)
	}
	if loaded.JetStream.AckWait != 9*time.Second || loaded.JetStream.PublishTimeout != 2*time.Second {
		t.Fatalf("stored JetStream durations changed: %#v", loaded.JetStream)
	}
}
