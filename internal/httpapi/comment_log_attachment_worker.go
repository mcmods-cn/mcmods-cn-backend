package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

const (
	commentLogAttachmentLeaseTTL         = 10 * time.Minute
	commentLogAttachmentRecoveryInterval = 30 * time.Second
	commentLogAttachmentRecoveryBatch    = 100
)

type CommentLogAttachmentWorker struct {
	db       *pgxpool.Pool
	queue    *queue.Client
	api      *Server
	workerID string
}

type claimedCommentLogAttachmentJob struct {
	id, commentID, fileID, requestedBy int64
	attempts, maxAttempts              int
	lockToken                          string
}

func NewCommentLogAttachmentWorker(cfg config.Config, db *pgxpool.Pool, queueClient *queue.Client) *CommentLogAttachmentWorker {
	return &CommentLogAttachmentWorker{
		db: db, queue: queueClient, api: &Server{cfg: cfg, db: db, queue: queueClient},
		workerID: "comment-log-worker-" + newExportID(),
	}
}

func (worker *CommentLogAttachmentWorker) Start(ctx context.Context) error {
	if worker == nil || worker.db == nil {
		return queue.ErrUnavailable
	}
	_, recoveryErr := worker.recoverDueJobs(ctx)
	go worker.recoveryLoop(ctx)
	if worker.queue == nil {
		return errors.Join(recoveryErr, queue.ErrUnavailable)
	}
	subscribeErr := worker.queue.SubscribeTask("comment_log_attachment", worker.handleMessage)
	return errors.Join(recoveryErr, subscribeErr)
}

func (worker *CommentLogAttachmentWorker) recoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(commentLogAttachmentRecoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := worker.recoverDueJobs(ctx); err != nil {
				slog.Error("recover comment log attachment jobs", "module", "comments", "error", err)
			}
		}
	}
}

func (worker *CommentLogAttachmentWorker) handleMessage(ctx context.Context, raw []byte) error {
	var message commentLogAttachmentJobMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		return err
	}
	if message.JobID <= 0 {
		return errors.New("comment log attachment job ID is missing")
	}
	return worker.processJob(ctx, message.JobID)
}

func (worker *CommentLogAttachmentWorker) processJob(ctx context.Context, jobID int64) error {
	job, claimed, err := worker.claimJob(ctx, jobID)
	if err != nil || !claimed {
		return err
	}
	var publicFileID string
	workErr := worker.db.QueryRow(ctx, `select file.public_id
		from comment_log_attachment_jobs job
		join comment_attachments attachment on attachment.comment_id=job.comment_id and attachment.attachment_file_id=job.attachment_file_id
		join oss_files file on file.id=attachment.attachment_file_id
		where job.id=$1 and job.status='processing' and job.locked_by=$2`, job.id, job.lockToken).Scan(&publicFileID)
	var result map[string]any
	if workErr == nil {
		result, workErr = worker.api.createFileLogShare(ctx, job.requestedBy, publicFileID, time.Now().UTC().AddDate(1, 0, 0))
	}
	publicCode := ""
	if workErr == nil {
		publicCode, _ = result["publicCode"].(string)
		if publicCode == "" {
			workErr = errors.New("log share public code is missing")
		}
	}
	if workErr == nil {
		workErr = worker.completeJob(ctx, job, publicCode)
	}
	if workErr == nil {
		return nil
	}
	persistErr := worker.failJob(ctx, job, workErr)
	return errors.Join(workErr, persistErr)
}

func (worker *CommentLogAttachmentWorker) claimJob(ctx context.Context, jobID int64) (claimedCommentLogAttachmentJob, bool, error) {
	job := claimedCommentLogAttachmentJob{id: jobID, lockToken: worker.workerID + ":" + newExportID()}
	err := worker.db.QueryRow(ctx, `update comment_log_attachment_jobs set
		status='processing',attempts=attempts+1,started_at=coalesce(started_at,now()),finished_at=null,
		locked_by=$2,lease_expires_at=now()+$3::interval,updated_at=now()
		where id=$1 and attempts<max_attempts and (
			(status='queued' and next_attempt_at<=now()) or
			(status='processing' and coalesce(lease_expires_at,updated_at)<=now())
		)
		returning comment_id,attachment_file_id,requested_by,attempts,max_attempts`,
		jobID, job.lockToken, pgInterval(commentLogAttachmentLeaseTTL)).Scan(
		&job.commentID, &job.fileID, &job.requestedBy, &job.attempts, &job.maxAttempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return claimedCommentLogAttachmentJob{}, false, nil
	}
	if err != nil {
		return claimedCommentLogAttachmentJob{}, false, err
	}
	return job, true, nil
}

func (worker *CommentLogAttachmentWorker) completeJob(ctx context.Context, job claimedCommentLogAttachmentJob, publicCode string) error {
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var shareID int64
	err = tx.QueryRow(ctx, `select share.id from log_shares share
		where share.public_code=$1 and share.owner_user_id=$2 and share.source_file_id=$3
		  and share.status='ready' and share.deleted_at is null and share.expires_at>now()
		for update`, publicCode, job.requestedBy, job.fileID).Scan(&shareID)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `insert into comment_log_bindings(comment_id,attachment_file_id,log_share_id)
		values($1,$2,$3)
		on conflict(comment_id,attachment_file_id) do update set log_share_id=excluded.log_share_id`,
		job.commentID, job.fileID, shareID)
	if err != nil || tag.RowsAffected() != 1 {
		return errors.Join(err, errors.New("comment log binding was not persisted"))
	}
	tag, err = tx.Exec(ctx, `update comment_attachments set kind='log',processing_status='ready'
		where comment_id=$1 and attachment_file_id=$2`, job.commentID, job.fileID)
	if err != nil || tag.RowsAffected() != 1 {
		return errors.Join(err, errors.New("comment log attachment was not finalized"))
	}
	tag, err = tx.Exec(ctx, `update comment_log_attachment_jobs set
		status='completed',next_attempt_at=now(),lease_expires_at=null,locked_by='',last_error='',finished_at=now(),updated_at=now()
		where id=$1 and status='processing' and locked_by=$2`, job.id, job.lockToken)
	if err != nil || tag.RowsAffected() != 1 {
		return errors.Join(err, errors.New("comment log attachment job lease was lost during completion"))
	}
	return tx.Commit(ctx)
}

func (worker *CommentLogAttachmentWorker) failJob(ctx context.Context, job claimedCommentLogAttachmentJob, cause error) error {
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var attempts, maxAttempts int
	err = tx.QueryRow(ctx, `select attempts,max_attempts from comment_log_attachment_jobs
		where id=$1 and status='processing' and locked_by=$2 for update`, job.id, job.lockToken).Scan(&attempts, &maxAttempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	lastError := truncateRunes(cause.Error(), 1000)
	if attempts >= maxAttempts {
		if _, err = tx.Exec(ctx, `update comment_log_attachment_jobs set
			status='failed',lease_expires_at=null,locked_by='',last_error=$3,finished_at=now(),updated_at=now()
			where id=$1 and status='processing' and locked_by=$2`, job.id, job.lockToken, lastError); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `update comment_attachments set kind='log',processing_status='failed'
			where comment_id=$1 and attachment_file_id=$2`, job.commentID, job.fileID); err != nil {
			return err
		}
	} else {
		delay := commentLogAttachmentRetryDelay(attempts)
		if _, err = tx.Exec(ctx, `update comment_log_attachment_jobs set
			status='queued',next_attempt_at=now()+$3::interval,lease_expires_at=null,locked_by='',last_error=$4,updated_at=now()
			where id=$1 and status='processing' and locked_by=$2`, job.id, job.lockToken, pgInterval(delay), lastError); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func commentLogAttachmentRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := 10 * time.Second
	for current := 1; current < attempts && delay < 10*time.Minute; current++ {
		delay *= 2
	}
	if delay > 10*time.Minute {
		return 10 * time.Minute
	}
	return delay
}

func (worker *CommentLogAttachmentWorker) recoverDueJobs(ctx context.Context) (int, error) {
	// A worker can crash after claiming the final attempt. These expired leases
	// cannot be claimed again, but must still leave processing and expose a
	// terminal failure to the UI.
	if _, err := worker.db.Exec(ctx, `with expired as materialized (
		select id from comment_log_attachment_jobs
		where status='processing' and attempts>=max_attempts and coalesce(lease_expires_at,updated_at)<=now()
		order by coalesce(lease_expires_at,updated_at),id for update skip locked limit $1
	), failed as (
		update comment_log_attachment_jobs job set status='failed',lease_expires_at=null,locked_by='',
		last_error='worker lease expired after the final attempt',finished_at=now(),updated_at=now()
		from expired where job.id=expired.id
		returning job.comment_id,job.attachment_file_id
	)
	update comment_attachments attachment set kind='log',processing_status='failed'
	from failed where attachment.comment_id=failed.comment_id and attachment.attachment_file_id=failed.attachment_file_id`, commentLogAttachmentRecoveryBatch); err != nil {
		return 0, err
	}
	rows, err := worker.db.Query(ctx, `select id from comment_log_attachment_jobs
		where (status='queued' and next_attempt_at<=now() and attempts<max_attempts)
		   or (status='processing' and coalesce(lease_expires_at,updated_at)<=now() and attempts<max_attempts)
		order by case when status='processing' then coalesce(lease_expires_at,updated_at) else next_attempt_at end,id
		limit $1`, commentLogAttachmentRecoveryBatch)
	if err != nil {
		return 0, err
	}
	jobIDs := make([]int64, 0, commentLogAttachmentRecoveryBatch)
	for rows.Next() {
		var jobID int64
		if err = rows.Scan(&jobID); err != nil {
			break
		}
		jobIDs = append(jobIDs, jobID)
	}
	rows.Close()
	if err == nil {
		err = rows.Err()
	}
	if err != nil {
		return 0, err
	}
	var recoveryErr error
	for _, jobID := range jobIDs {
		if err = worker.processJob(ctx, jobID); err != nil {
			recoveryErr = errors.Join(recoveryErr, fmt.Errorf("process comment log attachment job %d: %w", jobID, err))
		}
	}
	return len(jobIDs), recoveryErr
}
