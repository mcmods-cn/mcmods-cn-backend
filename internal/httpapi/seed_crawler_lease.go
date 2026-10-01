package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errSeedCrawlerLeaseLost = errors.New("seed crawler lease is no longer owned")

type seedCrawlerLeaseContextKey struct{}

type seedCrawlerLeaseIdentity struct {
	RunID int64
	Token string
}

func withSeedCrawlerLease(ctx context.Context, runID int64, token string) context.Context {
	return context.WithValue(ctx, seedCrawlerLeaseContextKey{}, seedCrawlerLeaseIdentity{RunID: runID, Token: token})
}

// Ordinary user requests have no crawler lease. Seed-derived writes lock their
// claimed run in the same transaction as the side effect, so a replacement
// claimant cannot take ownership between validation and commit.
func lockSeedCrawlerLeaseTx(ctx context.Context, tx pgx.Tx) error {
	lease, present := ctx.Value(seedCrawlerLeaseContextKey{}).(seedCrawlerLeaseIdentity)
	if !present {
		return nil
	}
	if lease.RunID <= 0 || lease.Token == "" || tx == nil {
		return errSeedCrawlerLeaseLost
	}
	var id int64
	err := tx.QueryRow(ctx, `select id from seed_crawler_runs
		where id=$1 and status='running' and lease_owner=$2 and lease_expires_at>clock_timestamp()
		for share`, lease.RunID, lease.Token).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return errSeedCrawlerLeaseLost
	}
	return err
}

func refreshSeedCrawlerLease(ctx context.Context, db *pgxpool.Pool, runID int64, token string) error {
	if db == nil || runID <= 0 || token == "" {
		return errSeedCrawlerLeaseLost
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("refresh seed crawler lease: %w", err)
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `select id from seed_crawler_runs
		where id=$1 and status='running' and lease_owner=$2 and lease_expires_at>clock_timestamp()
		for update skip locked`, runID, token).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// A fenced resource transaction can hold FOR SHARE on our own run.
		// Avoid timing out the heartbeat on that transaction: verify its live
		// identity without locking, and retry renewal on the next tick.
		var live bool
		if err = tx.QueryRow(ctx, `select exists(select 1 from seed_crawler_runs
			where id=$1 and status='running' and lease_owner=$2 and lease_expires_at>clock_timestamp())`, runID, token).Scan(&live); err != nil {
			return fmt.Errorf("verify locked seed crawler lease: %w", err)
		}
		if !live {
			return errSeedCrawlerLeaseLost
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock seed crawler heartbeat: %w", err)
	}
	tag, err := tx.Exec(ctx, `update seed_crawler_runs set lease_expires_at=clock_timestamp()+interval '5 minutes'
		where id=$1 and status='running' and lease_owner=$2 and lease_expires_at>clock_timestamp()`, runID, token)
	if err != nil {
		return fmt.Errorf("refresh seed crawler lease: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errSeedCrawlerLeaseLost
	}
	return tx.Commit(ctx)
}

func maintainSeedCrawlerLease(ctx context.Context, db *pgxpool.Pool, runID int64, token string) (context.Context, func()) {
	return maintainSeedCrawlerLeaseInterval(ctx, db, runID, token, 15*time.Second)
}

// The interval is injected internally for deterministic heartbeat tests; the
// production entry point above always uses the same bounded 15-second period.
func maintainSeedCrawlerLeaseInterval(ctx context.Context, db *pgxpool.Pool, runID int64, token string, interval time.Duration) (context.Context, func()) {
	leasedCtx, cancel := context.WithCancel(withSeedCrawlerLease(ctx, runID, token))
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-leasedCtx.Done():
				return
			case <-ticker.C:
				refreshCtx, stop := context.WithTimeout(leasedCtx, 10*time.Second)
				err := refreshSeedCrawlerLease(refreshCtx, db, runID, token)
				stop()
				if err != nil {
					return
				}
			}
		}
	}()
	return leasedCtx, func() {
		cancel()
		<-done
	}
}

func recoverExpiredSeedCrawlerRuns(ctx context.Context, db *pgxpool.Pool) error {
	_, err := db.Exec(ctx, `update seed_crawler_runs
		set status=case when attempts>=5 then 'failed' else 'pending' end,
			lease_owner='',lease_expires_at=null,next_attempt_at=now(),
			finished_at=case when attempts>=5 then now() else finished_at end,
			last_error=case when last_error='' then 'worker lease expired' else last_error end
		where status='running' and stats->>'kind' is distinct from 'translation_recovery'
			and (lease_expires_at is null or lease_expires_at<=clock_timestamp())`)
	return err
}
