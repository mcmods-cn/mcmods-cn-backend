package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestMinecraftServerReviewListQueryIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run the server review integration test")
		}
		databaseURL = config.Load().DB.ConnString()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	request, err := parseServerReviewPageRequest(nil)
	if err != nil {
		t.Fatal(err)
	}
	query, arguments := serverReviewPageSQL(request)
	rows, err := pool.Query(ctx, query, arguments...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var values struct {
			id, name, address, summary, primaryTag, loader           string
			reviewStatus, reviewNote, submitterID, submitterUsername string
			minecraftVersions, languages                             []string
			dedicatedClient, whitelist, onlineMode, modded           bool
			proofFileCount, linkCount, modCount                      int64
			createdAt                                                time.Time
			reviewedAt                                               *time.Time
		}
		if err = rows.Scan(
			&values.id, &values.name, &values.address, &values.summary,
			&values.minecraftVersions, &values.dedicatedClient, &values.languages,
			&values.primaryTag, &values.whitelist, &values.onlineMode, &values.modded,
			&values.loader, &values.reviewStatus, &values.reviewNote,
			&values.submitterID, &values.submitterUsername, &values.createdAt, &values.reviewedAt,
			new(int64), &values.proofFileCount, &values.linkCount, &values.modCount,
		); err != nil {
			t.Fatal(err)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}
