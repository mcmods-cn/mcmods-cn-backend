package httpapi

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSeedCrawlerLeaseFencingAndHeartbeatIntegration(t *testing.T) {
	url := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires dedicated migrated PostgreSQL via MCMODS_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := "apia_seed_" + randomHex(8)
	schema := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "create schema "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, "drop schema "+schema+" cascade"); err != nil {
			t.Errorf("allocated crawler lease fixture cleanup: %v", err)
		}
	}()
	if _, err = admin.Exec(ctx, "create table "+schema+".seed_crawler_runs (like public.seed_crawler_runs including all)"); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	cfg.ConnConfig.RuntimeParams["application_name"] = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var runID int64
	token := randomHex(16)
	if err = pool.QueryRow(ctx, `insert into seed_crawler_runs(status,lease_owner,lease_expires_at,attempts)
		values('running',$1,clock_timestamp()+interval '1 minute',1) returning id`, token).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	leased := withSeedCrawlerLease(ctx, runID, token)
	if err = lockSeedCrawlerLeaseTx(ctx, nil); err != nil {
		t.Fatalf("ordinary user write acquired a seed lease: %v", err)
	}
	if err = lockSeedCrawlerLeaseTx(withSeedCrawlerLease(ctx, 0, ""), nil); !errors.Is(err, errSeedCrawlerLeaseLost) {
		t.Fatalf("malformed internal lease silently became an ordinary request: %v", err)
	}
	for _, entry := range []struct {
		name, token string
		allowed     bool
	}{{"owner", token, true}, {"replacement", randomHex(16), false}} {
		t.Run(entry.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			err = lockSeedCrawlerLeaseTx(withSeedCrawlerLease(ctx, runID, entry.token), tx)
			if (err == nil) != entry.allowed {
				t.Fatalf("claim fence allowed=%v err=%v", entry.allowed, err)
			}
		})
	}
	if err = refreshSeedCrawlerLease(ctx, pool, runID, token); err != nil {
		t.Fatal(err)
	}
	var renewed bool
	if err = pool.QueryRow(ctx, `select lease_expires_at>clock_timestamp()+interval '4 minutes' from seed_crawler_runs where id=$1`, runID).Scan(&renewed); err != nil || !renewed {
		t.Fatalf("heartbeat did not extend live lease: %v %v", renewed, err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err = lockSeedCrawlerLeaseTx(leased, tx); err != nil {
		t.Fatal(err)
	}
	// A fenced write holds FOR SHARE. Its own heartbeat must not self-block
	// or cancel the live run while that short transaction is being committed.
	if err = refreshSeedCrawlerLease(ctx, pool, runID, token); err != nil {
		t.Fatalf("heartbeat treated its own fenced transaction as lease loss: %v", err)
	}
	replaced := make(chan error, 1)
	newToken := randomHex(16)
	go func() {
		_, e := pool.Exec(ctx, `update seed_crawler_runs set lease_owner=$2 where id=$1`, runID, newToken)
		replaced <- e
	}()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting bool
		if err = admin.QueryRow(ctx, `select exists(select 1 from pg_stat_activity where application_name=$1 and wait_event_type='Lock')`, name).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-replaced:
			t.Fatalf("replacement passed the fenced transaction: %v", err)
		case <-deadline.C:
			t.Fatal("did not observe replacement waiting on PostgreSQL's row lock")
		case <-tick.C:
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-replaced; err != nil {
		t.Fatal(err)
	}
	if err = refreshSeedCrawlerLease(ctx, pool, runID, token); !errors.Is(err, errSeedCrawlerLeaseLost) {
		t.Fatalf("replaced worker renewed the new worker's lease: %v", err)
	}
	heartbeatCtx, stop := maintainSeedCrawlerLeaseInterval(ctx, pool, runID, token, time.Millisecond)
	defer stop()
	select {
	case <-heartbeatCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("lost ownership did not cancel the old run's derived operations")
	}
	late, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer late.Rollback(ctx)
	if err = lockSeedCrawlerLeaseTx(leased, late); !errors.Is(err, errSeedCrawlerLeaseLost) {
		t.Fatalf("late old worker passed publication fence: %v", err)
	}
	if _, err = pool.Exec(ctx, `update seed_crawler_runs set lease_expires_at=clock_timestamp()-interval '1 second' where id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	if err = refreshSeedCrawlerLease(ctx, pool, runID, newToken); !errors.Is(err, errSeedCrawlerLeaseLost) {
		t.Fatalf("expired owner resurrected its lease: %v", err)
	}
	if err = recoverExpiredSeedCrawlerRuns(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var status, currentToken string
	if err = pool.QueryRow(ctx, `select status,lease_owner from seed_crawler_runs where id=$1`, runID).Scan(&status, &currentToken); err != nil || status != "pending" || currentToken != "" {
		t.Fatalf("expired lease was not recovered: %q %q %v", status, currentToken, err)
	}
	if _, err = pool.Exec(ctx, `update seed_crawler_runs set status='running',attempts=5,lease_owner=$2,lease_expires_at=clock_timestamp()-interval '1 second' where id=$1`, runID, token); err != nil {
		t.Fatal(err)
	}
	if err = recoverExpiredSeedCrawlerRuns(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select status from seed_crawler_runs where id=$1`, runID).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("exhausted crash attempts retried indefinitely: %q %v", status, err)
	}
	if _, err = pool.Exec(ctx, `update seed_crawler_runs set status='running',attempts=1,lease_owner=$2,
		lease_expires_at=clock_timestamp()-interval '1 second',stats='{"kind":"translation_recovery"}' where id=$1`, runID, token); err != nil {
		t.Fatal(err)
	}
	if err = recoverExpiredSeedCrawlerRuns(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select status,lease_owner from seed_crawler_runs where id=$1`, runID).Scan(&status, &currentToken); err != nil || status != "running" || currentToken != token {
		t.Fatalf("ordinary crawler recovery reclaimed translation-only recovery run: %q %q %v", status, currentToken, err)
	}
}

func TestSeedCrawlerHeartbeatStopsOnParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	derived, stop := maintainSeedCrawlerLease(ctx, nil, 1, "fixture")
	cancel()
	stop() // joins the goroutine without waiting for the 15-second timer
	if !errors.Is(derived.Err(), context.Canceled) {
		t.Fatalf("parent cancellation did not reach the leased context: %v", derived.Err())
	}
}
