package httpapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02CreatorAvatarReadsSettingsOnItsSingleConnectionTransactionIntegration(t *testing.T) {
	fixture := oct02AITestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := fixture.Exec(ctx, `create table oss_files(id bigint primary key,public_id text, uploader_id bigint, object_key text, original_name text,
		content_type text,size_bytes bigint,status text,scan_status text);
		insert into oss_files values(1,'c00000042',42,'avatar.png','avatar.png','image/png',12,'active','clean')`); err != nil {
		t.Fatal(err)
	}
	pc := fixture.Config()
	pc.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	server := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: "synthetic-creator-setting-key-at-least-32-characters"}}
	raw, err := json.Marshal(ossConfigPayload{PublicEndpoint: "https://images.example.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := security.EncryptSetting(server.cfg.SettingsEncryptionKey, raw)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)`, string(sealed)); err != nil {
		t.Fatal(err)
	}
	fileID := "c00000042"
	snapshot := creatorSnapshot{Kind: "author", Name: "Fixture", AvatarFileID: &fileID}
	if err := server.resolveCreatorAvatarTx(ctx, tx, 42, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.AvatarURL != "https://images.example.invalid/avatar.png" || snapshot.AvatarInternalID == nil || *snapshot.AvatarInternalID != 1 {
		t.Fatalf("transaction did not resolve owned raster and its uncommitted settings: %#v", snapshot)
	}
	if _, err := tx.Exec(ctx, `select 1`); err != nil {
		t.Fatalf("settings lookup damaged owning transaction: %v", err)
	}
}
