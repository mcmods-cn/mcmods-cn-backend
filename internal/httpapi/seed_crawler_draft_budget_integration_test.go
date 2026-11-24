package httpapi

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/config"
)

func TestSeedCrawlerDailyImportsCountFirstCommittedDraftAcrossWorkers(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires a task-owned PostgreSQL instance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := newTEST020IsolatedDatabase(t, ctx)
	worker := NewSeedCrawlerWorker(config.Load(), pool)
	var runID int64
	if err := pool.QueryRow(ctx, "insert into seed_crawler_runs(dry_run) values(false) returning id").Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "update seed_crawler_configs set daily_limit=1 where id"); err != nil {
		t.Fatal(err)
	}
	candidate := func(externalID string) int64 {
		t.Helper()
		id, _, err := worker.upsertSeedCrawlerCandidate(ctx, runID, "mod", seedModrinthHit{ProjectID: externalID, Title: "Example mod"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	first, second, yesterday := candidate("alpha"), candidate("beta"), candidate("yesterday")
	if _, err := pool.Exec(ctx, `update seed_crawler_candidates set status='submitted',drafted_at=now()-interval '2 days',
		updated_at=now()-interval '1 day' where id=$1`, yesterday); err != nil {
		t.Fatal(err)
	}
	candidate("yesterday") // Discovering an existing project updates its observation time.
	requireUsage := func(want int) {
		t.Helper()
		used, _, err := seedCrawlerDraftsToday(ctx, pool)
		if err != nil || used != want {
			t.Fatalf("daily imports = %d, want %d, err=%v", used, want, err)
		}
	}
	begin := func() pgx.Tx {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
		return tx
	}
	requireUsage(0)
	reverted := begin()
	if staged, err := stageSeedCrawlerCandidateTx(ctx, reverted, first, "submitting"); err != nil || !staged {
		t.Fatalf("first draft staging failed: staged=%v err=%v", staged, err)
	}
	if err := reverted.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	requireUsage(0)

	leader, follower := begin(), begin()
	if staged, err := stageSeedCrawlerCandidateTx(ctx, leader, first, "submitting"); err != nil || !staged {
		t.Fatalf("first writer: staged=%v err=%v", staged, err)
	}
	done := make(chan error, 1)
	finished := make(chan struct{})
	followerPID := follower.Conn().PgConn().PID()
	go func() {
		defer close(finished)
		_, err := stageSeedCrawlerCandidateTx(ctx, follower, second, "draft")
		done <- err
	}()
	defer func() {
		_ = leader.Rollback(ctx)
		<-finished
	}()
	wait := time.NewTicker(10 * time.Millisecond)
	defer wait.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, "select $1::int=any(pg_blocking_pids($2::int))",
			leader.Conn().PgConn().PID(), followerPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("second draft finished before first transaction settled: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-wait.C:
		}
	}
	if err := leader.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, errSeedCrawlerDailyImportLimit) {
		t.Fatalf("second worker received %v", err)
	}
	if err := follower.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	requireUsage(1)
	var chargedAt time.Time
	if err := pool.QueryRow(ctx, "select drafted_at from seed_crawler_candidates where id=$1", first).Scan(&chargedAt); err != nil {
		t.Fatal(err)
	}
	retry := begin()
	if staged, err := stageSeedCrawlerCandidateTx(ctx, retry, first, "submitting"); err != nil || !staged {
		t.Fatalf("retry of the same staged candidate: staged=%v err=%v", staged, err)
	}
	if err := retry.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var same bool
	if err := pool.QueryRow(ctx, "select drafted_at=$2 from seed_crawler_candidates where id=$1", first, chargedAt).Scan(&same); err != nil || !same {
		t.Fatalf("retry replaced the original first-draft date: same=%v err=%v", same, err)
	}
	requireUsage(1)
}
