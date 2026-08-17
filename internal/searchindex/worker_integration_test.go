package searchindex

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestLoadServerDocumentsQueryIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate the server search projection against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	documents, err := NewWorker(pool, nil).loadServerDocuments(ctx, []int64{})
	if err != nil {
		t.Fatalf("load empty server search projection: %v", err)
	}
	if len(documents) != 0 {
		t.Fatalf("empty ID selection returned %d server documents", len(documents))
	}
}
