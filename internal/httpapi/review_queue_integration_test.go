package httpapi

import (
	"context"
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

	rows, err := pool.Query(ctx, contentReviewQueueQuery, []string{}, true, true, int64(0))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var item modContentReviewItem
		if err = rows.Scan(
			&item.ID, &item.Source, &item.Category, &item.Operation, &item.AggregateType,
			&item.ProjectType, &item.ProjectID, &item.ModSiteID, &item.ModName, &item.SubmittedBy,
			&item.UserID, &item.Username, &item.Title, &item.Summary, &item.CreatedAt, &item.RequiresGlobal,
		); err != nil {
			t.Fatal(err)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}
