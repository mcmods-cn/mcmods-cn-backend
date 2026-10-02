package httpapi

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var errProjectAutomationLeaseLost = errors.New("project automation lease is no longer current")

func lockProjectAutomationRunTx(ctx context.Context, tx pgx.Tx, job projectAutomationJob) error {
	// Synchronous domain helpers are also used without an asynchronous run.
	// Every runtime execute path receives a nonzero RunID from processOne.
	if job.RunID == 0 {
		return nil
	}
	var id int64
	err := tx.QueryRow(ctx, `select run.id from project_auto_update_runs run
	 join project_auto_update_settings setting on setting.id=run.setting_id
	 join project_external_sources source on source.project_route_id=setting.project_route_id and source.source_type=setting.source_type
	 where run.id=$1 and run.status='running' and run.lease_owner=$2 and run.lease_expires_at>now()
	 and setting.id=$3 and setting.project_route_id=$4 and setting.update_kind=$5 and setting.source_type=$6
	 and source.external_project_id=$7 and setting.license_override=$8
	 and setting.license_override_reason=$9 and setting.license_override_source=$10
	 for update of run for share of setting,source`, job.RunID, job.LeaseOwner, job.SettingID, job.RouteID, job.Kind, job.SourceType, job.ExternalID, job.LicenseOverride, job.OverrideReason, job.OverrideSource).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return errProjectAutomationLeaseLost
	}
	return err
}

func (worker *ProjectAutomationWorker) mutateProjectAutomationRun(ctx context.Context, job projectAutomationJob, statement string, args ...any) (pgconn.CommandTag, error) {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockProjectAutomationRunTx(ctx, tx, job); err != nil {
		return pgconn.CommandTag{}, err
	}
	tag, err := tx.Exec(ctx, statement, args...)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return pgconn.CommandTag{}, err
	}
	return tag, nil
}

func (worker *ProjectAutomationWorker) recoverExpiredLeases(ctx context.Context) error {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `select run.id,setting.id,run.attempts,setting.interval_code
		from project_auto_update_runs run join project_auto_update_settings setting on setting.id=run.setting_id
		where run.status='running' and (run.lease_expires_at is null or run.lease_expires_at<=now())
		order by run.id for update of run,setting skip locked limit 50`)
	if err != nil {
		return err
	}
	type expired struct {
		run, setting int64
		attempts     int
		interval     string
	}
	items := make([]expired, 0, 50)
	for rows.Next() {
		var item expired
		if err = rows.Scan(&item.run, &item.setting, &item.attempts, &item.interval); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err = finishRows(rows); err != nil {
		return err
	}
	for _, item := range items {
		status := "pending"
		if item.attempts >= 5 {
			status = "dead_letter"
		}
		if _, err = tx.Exec(ctx, `update project_auto_update_runs set status=$2,lease_owner='',lease_expires_at=null,
			next_attempt_at=now(),last_error_code='lease_expired',last_error='worker lease expired',
			finished_at=case when $2='dead_letter' then now() else null end where id=$1`, item.run, status); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `update project_auto_update_settings set last_run_at=now(),last_status=$2,
			last_error_code='lease_expired',last_error='worker lease expired',
			next_run_at=case when $2='dead_letter' then $3 else next_run_at end where id=$1`, item.setting, status, autoUpdateNextRun(item.interval, time.Now().UTC())); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
