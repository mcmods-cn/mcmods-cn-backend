package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errProjectAutomationLeaseLost = errors.New("project automation lease is no longer owned")

type projectAutomationLeaseContextKey struct{}

type projectAutomationLeaseIdentity struct {
	RunID int64
	Token string
}

func withProjectAutomationLease(ctx context.Context, runID int64, token string) context.Context {
	return context.WithValue(ctx, projectAutomationLeaseContextKey{}, projectAutomationLeaseIdentity{RunID: runID, Token: token})
}

// Ordinary user requests have no automation lease. Automation-derived writes lock their
// claimed run in the same transaction as the side effect, so a replacement
// claimant cannot take ownership between validation and commit.
func lockProjectAutomationLeaseTx(ctx context.Context, tx pgx.Tx) error {
	lease, present := ctx.Value(projectAutomationLeaseContextKey{}).(projectAutomationLeaseIdentity)
	if !present {
		return nil
	}
	if lease.RunID <= 0 || lease.Token == "" || tx == nil {
		return errProjectAutomationLeaseLost
	}
	var id int64
	err := tx.QueryRow(ctx, `select id from project_auto_update_runs
		where id=$1 and status='running' and lease_owner=$2 and lease_expires_at>clock_timestamp()
		for share`, lease.RunID, lease.Token).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return errProjectAutomationLeaseLost
	}
	return err
}

func refreshProjectAutomationLease(ctx context.Context, db *pgxpool.Pool, runID int64, token string) error {
	if db == nil || runID <= 0 || token == "" {
		return errProjectAutomationLeaseLost
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("refresh project automation lease: %w", err)
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `select id from project_auto_update_runs
		where id=$1 and status='running' and lease_owner=$2 and lease_expires_at>clock_timestamp()
		for update skip locked`, runID, token).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// A fenced resource transaction can hold FOR SHARE on our own run.
		// Avoid timing out the heartbeat on that transaction: verify its live
		// identity without locking, and retry renewal on the next tick.
		var live bool
		if err = tx.QueryRow(ctx, `select exists(select 1 from project_auto_update_runs
			where id=$1 and status='running' and lease_owner=$2 and lease_expires_at>clock_timestamp())`, runID, token).Scan(&live); err != nil {
			return fmt.Errorf("verify locked project automation lease: %w", err)
		}
		if !live {
			return errProjectAutomationLeaseLost
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock project automation heartbeat: %w", err)
	}
	tag, err := tx.Exec(ctx, `update project_auto_update_runs set lease_expires_at=clock_timestamp()+interval '10 minutes'
		where id=$1 and status='running' and lease_owner=$2 and lease_expires_at>clock_timestamp()`, runID, token)
	if err != nil {
		return fmt.Errorf("refresh project automation lease: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errProjectAutomationLeaseLost
	}
	return tx.Commit(ctx)
}

func maintainProjectAutomationLease(ctx context.Context, db *pgxpool.Pool, runID int64, token string) (context.Context, func()) {
	return maintainProjectAutomationLeaseInterval(ctx, db, runID, token, 15*time.Second)
}

// The interval is injected internally for deterministic heartbeat tests; the
// production entry point above always uses the same bounded 15-second period.
func maintainProjectAutomationLeaseInterval(ctx context.Context, db *pgxpool.Pool, runID int64, token string, interval time.Duration) (context.Context, func()) {
	leasedCtx, cancel := context.WithCancel(withProjectAutomationLease(ctx, runID, token))
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
				err := refreshProjectAutomationLease(refreshCtx, db, runID, token)
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

func recoverExpiredProjectAutomationRuns(ctx context.Context, db *pgxpool.Pool) error {
	_, err := db.Exec(ctx, `update project_auto_update_runs
		set status=case when attempts>=5 then 'dead_letter' else 'pending' end,
			lease_owner='',lease_expires_at=null,next_attempt_at=now(),last_error_code='lease_expired',
			finished_at=case when attempts>=5 then now() else finished_at end,
			last_error=case when last_error='' then 'worker lease expired' else last_error end
		where status='running' and (lease_expires_at is null or lease_expires_at<=clock_timestamp())`)
	return err
}
