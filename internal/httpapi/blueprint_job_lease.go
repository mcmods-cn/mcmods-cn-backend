package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	blueprintJobHeartbeatInterval = 15 * time.Second
	blueprintJobStaleAfter        = 5 * time.Minute
	blueprintJobMaxCrashAttempts  = 3
)

var errBlueprintJobLeaseLost = errors.New("blueprint job lease was lost")

type blueprintJobClaim struct {
	BlueprintID, CreatedBy            int64
	Operation, TargetFormat, RunToken string
}

func (worker *BlueprintWorker) claimBlueprintJob(ctx context.Context, jobID int64) (blueprintJobClaim, error) {
	claim := blueprintJobClaim{RunToken: randomHex(16)}
	err := worker.db.QueryRow(ctx, `update blueprint_jobs
		set status='processing',progress=1,attempts=attempts+1,started_at=now(),finished_at=null,
		 heartbeat_at=now(),updated_at=now(),run_token=$2,last_error=''
		where id=$1 and status='queued'
		returning blueprint_id,operation,target_format,coalesce(created_by,0)`, jobID, claim.RunToken).
		Scan(&claim.BlueprintID, &claim.Operation, &claim.TargetFormat, &claim.CreatedBy)
	return claim, err
}

func (worker *BlueprintWorker) recoverStaleBlueprintJobs(ctx context.Context) error {
	_, err := worker.db.Exec(ctx, `with recovered as (update blueprint_jobs
		set status=case when attempts<$2 then 'queued' else 'failed' end,progress=0,run_token='',heartbeat_at=null,
		 started_at=null,finished_at=case when attempts<$2 then null else now() end,
		 last_error=case when attempts<$2 then '' else 'worker lease recovery limit reached' end,updated_at=now()
		where status='processing' and coalesce(heartbeat_at,updated_at)<now()-$1::interval
		returning id,blueprint_id,operation,status)
		update blueprints set status='failed',last_error='worker lease recovery limit reached',updated_at=now()
		where status='processing' and id in (select blueprint_id from recovered where operation='normalize' and status='failed')
		and not exists(select 1 from blueprint_jobs active_job where active_job.blueprint_id=blueprints.id
		 and active_job.operation='normalize' and active_job.status in('queued','processing')
		 and active_job.id not in(select id from recovered where status='failed'))`, pgInterval(blueprintJobStaleAfter), blueprintJobMaxCrashAttempts)
	return err
}

func (worker *BlueprintWorker) startBlueprintJobHeartbeat(parent context.Context, jobID int64, runToken string) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(blueprintJobHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tag, err := worker.db.Exec(ctx, `update blueprint_jobs set heartbeat_at=now(),updated_at=now()
					where id=$1 and run_token=$2 and status='processing'`, jobID, runToken)
				if err != nil {
					cancel(fmt.Errorf("refresh blueprint job lease: %w", err))
					return
				}
				if tag.RowsAffected() != 1 {
					cancel(errBlueprintJobLeaseLost)
					return
				}
			}
		}
	}()
	return ctx, func() {
		cancel(context.Canceled)
		<-done
	}
}

func (worker *BlueprintWorker) updateBlueprintJobProgress(ctx context.Context, jobID int64, runToken string, progress int) error {
	tag, err := worker.db.Exec(ctx, `update blueprint_jobs set progress=$3,heartbeat_at=now(),updated_at=now()
		where id=$1 and run_token=$2 and status='processing'`, jobID, runToken, progress)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errBlueprintJobLeaseLost
	}
	return nil
}

func lockBlueprintJobLeaseTx(ctx context.Context, tx pgx.Tx, jobID int64, runToken string) error {
	var id int64
	err := tx.QueryRow(ctx, `select id from blueprint_jobs where id=$1 and run_token=$2 and status='processing' for update`, jobID, runToken).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return errBlueprintJobLeaseLost
	}
	return err
}

// A publication transaction holds the job row, so a heartbeat cannot update
// it concurrently. Refresh at commit time to prevent immediate stale recovery
// after a long material write has legitimately held that lock.
func refreshBlueprintJobLeaseTx(ctx context.Context, tx pgx.Tx, jobID int64, runToken string) error {
	tag, err := tx.Exec(ctx, `update blueprint_jobs set heartbeat_at=clock_timestamp(),updated_at=clock_timestamp()
		where id=$1 and run_token=$2 and status='processing'`, jobID, runToken)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errBlueprintJobLeaseLost
	}
	return nil
}

func (worker *BlueprintWorker) finishBlueprintJob(ctx context.Context, jobID int64, runToken string, failure error) error {
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockBlueprintJobLeaseTx(ctx, tx, jobID, runToken); err != nil {
		return err
	}
	if failure == nil {
		_, err = tx.Exec(ctx, `update blueprint_jobs set status='completed',progress=100,finished_at=now(),
			heartbeat_at=now(),updated_at=now(),last_error='',run_token='' where id=$1`, jobID)
	} else {
		message := truncateRunes(failure.Error(), 1000)
		_, err = tx.Exec(ctx, `update blueprint_jobs set status='failed',finished_at=now(),heartbeat_at=now(),
			updated_at=now(),last_error=$2,run_token='' where id=$1`, jobID, message)
		if err == nil {
			// A failed conversion does not make other ready formats unusable.
			// A deleted blueprint must never be restored by a late worker.
			_, err = tx.Exec(ctx, `update blueprints set status='failed',last_error=$2,updated_at=now()
				where status<>'deleted' and id=(select blueprint_id from blueprint_jobs where id=$1 and operation='normalize')`, jobID, message)
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
