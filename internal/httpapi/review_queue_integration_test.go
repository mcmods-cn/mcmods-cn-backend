package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

// This integration test intentionally executes the complete UNION used by the
// review queue. It catches schema drift in any one of the supported content
// kinds even when that branch currently has no pending rows.
func TestContentReviewQueueQueryIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run the review queue integration test")
		}
		databaseURL = config.Load().DB.ConnString()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var itemsJSON, facetsJSON []byte
	var total int64
	if err = pool.QueryRow(ctx, contentReviewQueueQuery, []string{}, true, true, int64(0), "", "", "", "", 50, int64(0)).
		Scan(&itemsJSON, &total, &facetsJSON); err != nil {
		t.Fatal(err)
	}
	var items []modContentReviewItem
	if err = json.Unmarshal(itemsJSON, &items); err != nil {
		t.Fatal(err)
	}
	var facets map[string]json.RawMessage
	if err = json.Unmarshal(facetsJSON, &facets); err != nil {
		t.Fatal(err)
	}
	if total < int64(len(items)) || facets["categories"] == nil || facets["operations"] == nil || facets["projectTypes"] == nil {
		t.Fatalf("invalid queue aggregate: total=%d items=%d facets=%s", total, len(items), facetsJSON)
	}
}
