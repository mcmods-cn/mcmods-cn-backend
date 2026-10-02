package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestStickerMarkdownUsageQueryIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify sticker references against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `create temp table sticker_content_references(
		pack_code text not null,sticker_code text not null
	) on commit drop`); err != nil {
		t.Fatal(err)
	}
	used, err := stickerMarkdownIsUsed(ctx, tx, "[sticker:integration:missing]")
	if err != nil {
		t.Fatalf("query sticker references: %v", err)
	}
	if used {
		t.Fatal("nonexistent integration sticker unexpectedly has references")
	}
}
