package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestBUG041SeedCrawlerCandidateTracksCurrentObservationRunIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify seed crawler candidate provenance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	loaded := config.Load()
	poolConfig, err := pgxpool.ParseConfig(loaded.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var firstRunID, secondRunID int64
	if err = pool.QueryRow(ctx, `insert into seed_crawler_runs(public_id,dry_run) values('bug041a01',true) returning id`).Scan(&firstRunID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into seed_crawler_runs(public_id,dry_run) values('bug041a02',true) returning id`).Scan(&secondRunID); err != nil {
		t.Fatal(err)
	}

	worker := NewSeedCrawlerWorker(loaded, pool)
	firstCandidateID, firstStatus, err := worker.upsertSeedCrawlerCandidate(ctx, firstRunID, "mod", seedModrinthHit{
		ProjectID: "bug041-project", Slug: "bug041-old", Title: "Old observation", Downloads: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondCandidateID, secondStatus, err := worker.upsertSeedCrawlerCandidate(ctx, secondRunID, "mod", seedModrinthHit{
		ProjectID: "bug041-project", Slug: "bug041-new", Title: "New observation", Downloads: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if firstCandidateID != secondCandidateID || firstStatus != "candidate" || secondStatus != "candidate" {
		t.Fatalf("candidate upserts=%d/%s then %d/%s", firstCandidateID, firstStatus, secondCandidateID, secondStatus)
	}

	var firstSeenRunID, lastSeenRunID, downloads int64
	var payload json.RawMessage
	if err = pool.QueryRow(ctx, `select first_seen_run_id,last_seen_run_id,downloads,payload from seed_crawler_candidates where id=$1`, firstCandidateID).
		Scan(&firstSeenRunID, &lastSeenRunID, &downloads, &payload); err != nil {
		t.Fatal(err)
	}
	var currentHit seedModrinthHit
	if err = json.Unmarshal(payload, &currentHit); err != nil {
		t.Fatal(err)
	}
	if firstSeenRunID != firstRunID || lastSeenRunID != secondRunID || downloads != 20 || currentHit.Slug != "bug041-new" {
		t.Fatalf("candidate first/last/downloads/slug=%d/%d/%d/%q, want %d/%d/20/bug041-new",
			firstSeenRunID, lastSeenRunID, downloads, currentHit.Slug, firstRunID, secondRunID)
	}

	var detail seedCrawlerCandidateDetailResponse
	if err = pool.QueryRow(ctx, seedCrawlerCandidateDetailSQL, "bug041-project").Scan(
		&detail.ExternalProjectID, &detail.ProjectType, &detail.Downloads, &detail.Status,
		&detail.FirstSeenRunID, &detail.LastSeenRunID, &detail.Payload, &detail.LastError,
		&detail.CreatedAt, &detail.UpdatedAt, &detail.DraftID,
	); err != nil {
		t.Fatal(err)
	}
	if detail.FirstSeenRunID != "bug041a01" || detail.LastSeenRunID != "bug041a02" {
		t.Fatalf("candidate API provenance first/last=%v/%v", detail.FirstSeenRunID, detail.LastSeenRunID)
	}
	if _, err = pool.Exec(ctx, `delete from seed_crawler_runs where id=$1`, firstRunID); err == nil {
		t.Fatal("first-seen source run deletion unexpectedly orphaned candidate provenance")
	}
}
