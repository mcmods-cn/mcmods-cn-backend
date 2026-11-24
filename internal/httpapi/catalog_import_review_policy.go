package httpapi

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

func catalogReviewerAllowedTx(ctx context.Context, tx pgx.Tx, claims security.Claims, projectID string, submittedBy int64) (bool, error) {
	claims, err := resolveCurrentWriteActorTx(ctx, tx, claims)
	if errors.Is(err, errWriteAccountBanned) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return canReviewProjectSubmission(claims, projectID, submittedBy), nil
}

// The Worker may have completed before the target was archived. Ready-to-review
// is still unpublished work: the user's confirmed archive policy cancels that
// remainder, keeps its immutable source/audit, and requires a new upload. Never
// rewrite an already active revision or fabricate a review rejection decision.
// Caller owns the common target advisory lock before entering this transaction.
func cancelArchivedImportReviewTx(ctx context.Context, tx pgx.Tx, revisionID string, actorID int64) error {
	var jobID string
	err := tx.QueryRow(ctx, `select job.id from catalog_import_jobs job
 join catalog_import_revisions revision on revision.job_id=job.id
 where revision.id=$1 and not revision.is_active and revision.status in ('ready','partial')
 and job.status in ('ready','partial') and job.run_token='' for update of job`, revisionID).Scan(&jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		// A terminal or already-published attempt has no unpublished remainder
		// to cancel. The caller still rejects publication of the archived target.
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update catalog_import_revisions set status='rejected'
 where job_id=$1 and not is_active and status in ('ready','partial')`, jobID); err != nil {
		return err
	}
	const reason = "target_archived_reupload_required"
	if _, err = tx.Exec(ctx, `update catalog_import_jobs set status='cancelled',current_stage='cancelled',
 progress=least(progress,99),error_code=$2,error_detail=jsonb_build_object('message','Target archived before review; upload again after restoring it.'),
 finished_at=now(),heartbeat_at=now(),updated_at=now(),run_token='' where id=$1`, jobID, reason); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_job_logs(job_id,level,stage,message)
 values($1,'warn','cancelled',$2)`, jobID, reason); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into audit_events(aggregate_type,aggregate_key,actor_id,action,metadata)
 values('catalog_import_job',$1,$2,'execution_cancelled',jsonb_build_object(
 'reason',$3::text,'source','current_execution_policy','phase','before_independent_review',
 'triggering_revision',$4::text,'scope','unpublished_revisions'))`, jobID, actorID, reason, revisionID)
	return err
}
