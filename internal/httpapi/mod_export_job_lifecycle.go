package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	modExportHeartbeatInterval = 15 * time.Second
	modExportStaleAfter        = 5 * time.Minute
)

var errModExportLeaseLost = errors.New("mod export job lease was lost")

func (s *Server) claimModExportJob(ctx context.Context, jobID string) (string, error) {
	runToken := newExportID()
	tag, err := s.db.Exec(ctx, `
		update catalog_import_jobs
		set status='validating',progress=5,current_stage='download',started_at=now(),finished_at=null,
			heartbeat_at=now(),updated_at=now(),run_token=$2,attempt_count=attempt_count+1,
			error_code='',error_detail='{}'::jsonb
		where id=$1 and status='queued'`, jobID, runToken)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", errModExportLeaseLost
	}
	return runToken, nil
}

func (s *Server) startModExportHeartbeat(parent context.Context, jobID, runToken string) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(modExportHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tag, err := s.db.Exec(ctx, `update catalog_import_jobs set heartbeat_at=now(),updated_at=now() where id=$1 and run_token=$2 and status in ('validating','importing')`, jobID, runToken)
				if err == nil && tag.RowsAffected() == 0 {
					cancel(errModExportLeaseLost)
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

func (s *Server) updateModExportJob(ctx context.Context, jobID, runToken, status string, progress int, stage string) error {
	tag, err := s.db.Exec(ctx, `
		update catalog_import_jobs
		set status=$3,progress=$4,current_stage=$5,heartbeat_at=now(),updated_at=now()
		where id=$1 and run_token=$2`, jobID, runToken, status, progress, stage)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errModExportLeaseLost
	}
	return nil
}

func (s *Server) failModExportJob(jobID, runToken, code string, failure error) {
	detail, _ := json.Marshal(map[string]string{"message": failure.Error()})
	var updated bool
	for attempt := 0; attempt < 4; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		tag, err := s.db.Exec(ctx, `
			update catalog_import_jobs
			set status='failed',error_code=$3,error_detail=$4::jsonb,finished_at=now(),heartbeat_at=now(),updated_at=now(),run_token=''
			where id=$1 and run_token=$2`, jobID, runToken, code, string(detail))
		cancel()
		if err == nil {
			updated = tag.RowsAffected() > 0
			break
		}
		time.Sleep(time.Duration(attempt+1) * 250 * time.Millisecond)
	}
	if !updated {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = s.db.Exec(ctx, `insert into catalog_import_job_logs(job_id,level,stage,message) values($1,'error','failed',$2)`, jobID, failure.Error())
	s.notifyModExportResult(ctx, jobID, "failed", failure)
}

func (s *Server) recoverStaleModExportJobs(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `
		update catalog_import_jobs
		set status='queued',progress=0,current_stage='recovery',run_token='',heartbeat_at=null,
			started_at=null,finished_at=null,error_code='',error_detail='{}'::jsonb,updated_at=now()
		where status in ('validating','importing')
		  and coalesce(heartbeat_at,updated_at) < now() - $1::interval`, pgInterval(modExportStaleAfter))
	return err
}

func (s *Server) markStalledModExportJob(ctx context.Context, jobID string, modID int64) (bool, error) {
	detail, _ := json.Marshal(map[string]string{"message": "The import worker stopped updating this job. Retry the import."})
	tag, err := s.db.Exec(ctx, `
		update catalog_import_jobs
		set status='failed',error_code='worker_stalled',error_detail=$3::jsonb,finished_at=now(),run_token='',updated_at=now()
		where id=$1 and mod_id=$2 and status in ('validating','importing')
		  and coalesce(heartbeat_at,updated_at) < now() - $4::interval`, jobID, modID, string(detail), pgInterval(modExportStaleAfter))
	return err == nil && tag.RowsAffected() > 0, err
}

func (s *Server) resetModExportJobForRetry(ctx context.Context, jobID string, modID, actorID int64) (bool, error) {
	tag, err := s.db.Exec(ctx, `
		update catalog_import_jobs
		set status='queued',progress=0,current_stage='recovery',error_code='',error_detail='{}'::jsonb,
			created_by=$3,started_at=null,finished_at=null,heartbeat_at=null,run_token='',updated_at=now()
		where id=$1 and mod_id=$2 and (
			status in ('failed','cancelled') or
			(status in ('validating','importing') and coalesce(heartbeat_at,updated_at) < now() - $4::interval)
		)`, jobID, modID, actorID, pgInterval(modExportStaleAfter))
	return err == nil && tag.RowsAffected() > 0, err
}

func (s *Server) dispatchModExportJob(ctx context.Context, jobID string) {
	if s.cfg.NATS.OutboxEnabled {
		return
	}
	message := modExportJobMessage{JobID: jobID}
	if s.queue != nil && s.queue.PublishTask(ctx, modExportTaskCode, message) == nil {
		_, _ = s.db.Exec(ctx, `update nats_outbox set published_at=now() where aggregate_type='mod_export_job' and aggregate_id=$1 and published_at is null`, jobID)
		return
	}
	jobContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Hour)
	go func() {
		defer cancel()
		_ = s.importModExportJob(jobContext, jobID)
	}()
}

func (s *Server) runModExportTransaction(ctx context.Context, action func(pgx.Tx) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = action(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Server) cleanupModExportStaging(ctx context.Context, packageID string, modID int64, runToken string) error {
	if packageID == "" || modID <= 0 || runToken == "" {
		return nil
	}
	_, err := s.db.Exec(ctx, `delete from catalog_import_revisions where package_id=$1 and mod_id=$2 and status='staging' and import_run_token=$3`, packageID, modID, runToken)
	return err
}

func pgInterval(value time.Duration) string {
	return fmt.Sprintf("%f seconds", value.Seconds())
}
