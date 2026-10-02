package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

const (
	ossDeletionPollInterval = 15 * time.Second
	ossDeletionLeaseTimeout = 5 * time.Minute
	ossDeletionBatchSize    = 32
)

var (
	errOSSDeletionCredentialsUnavailable = errors.New("OSS credentials are unavailable")
	errOSSDeletionTargetIncomplete       = errors.New("OSS deletion target is incomplete")
	errOSSDeletionLeaseLost              = errors.New("OSS deletion job lease was lost")
)

type ossDeletionTarget struct {
	FileID    *int64
	Bucket    string
	Endpoint  string
	Region    string
	UseCName  bool
	ObjectKey string
	Reason    string
}

type ossDeletionJob struct {
	ID          int64
	FileID      *int64
	Bucket      string
	Endpoint    string
	Region      string
	UseCName    bool
	ObjectKey   string
	Attempts    int
	MaxAttempts int
	LockToken   string
}

type OSSDeletionWorker struct {
	server   *Server
	workerID string
}

func NewOSSDeletionWorker(cfg config.Config, db *pgxpool.Pool) *OSSDeletionWorker {
	return &OSSDeletionWorker{server: &Server{cfg: cfg, db: db}, workerID: "oss-deletion-worker-" + newExportID()}
}

func (worker *OSSDeletionWorker) Start(ctx context.Context) {
	if worker == nil || worker.server == nil || worker.server.db == nil {
		return
	}
	go worker.run(ctx)
}

func (worker *OSSDeletionWorker) run(ctx context.Context) {
	ticker := time.NewTicker(ossDeletionPollInterval)
	defer ticker.Stop()
	worker.drain(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.drain(ctx)
		}
	}
}

func (worker *OSSDeletionWorker) drain(ctx context.Context) {
	if _, err := worker.deadLetterExhaustedLeases(ctx); err != nil {
		log.Printf("dead-letter exhausted OSS deletion leases: %v", err)
		return
	}
	for processed := 0; processed < ossDeletionBatchSize; processed++ {
		job, err := worker.claim(ctx)
		if errors.Is(err, pgx.ErrNoRows) || ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("claim OSS deletion outbox job: %v", err)
			return
		}
		if err = worker.deleteObject(ctx, job); err != nil {
			if ctx.Err() != nil {
				return
			}
			dead, persistErr := worker.recordFailure(ctx, job, err)
			if persistErr != nil {
				log.Printf("persist OSS deletion failure for job %d: %v", job.ID, persistErr)
			} else if dead {
				log.Printf("OSS deletion job %d entered dead state after attempt %d/%d: %v", job.ID, job.Attempts, job.MaxAttempts, err)
			}
			continue
		}
		if err = worker.complete(ctx, job); err != nil {
			log.Printf("complete OSS deletion outbox job %d: %v", job.ID, err)
		}
	}
}

func (worker *OSSDeletionWorker) claim(ctx context.Context) (ossDeletionJob, error) {
	job := ossDeletionJob{LockToken: worker.workerID + ":" + newExportID()}
	err := worker.server.db.QueryRow(ctx, `update oss_object_deletion_outbox set
		status='processing',attempts=attempts+1,locked_at=now(),locked_by=$2,updated_at=now()
		where id=(select id from oss_object_deletion_outbox
			where attempts<max_attempts and ((status='pending' and next_attempt_at<=now())
			   or (status='processing' and locked_at<now()-make_interval(secs => $1)))
			order by next_attempt_at,id for update skip locked limit 1)
		returning id,oss_file_id,bucket,endpoint,region,use_cname,object_key,attempts,max_attempts`,
		int(ossDeletionLeaseTimeout/time.Second), job.LockToken).
		Scan(&job.ID, &job.FileID, &job.Bucket, &job.Endpoint, &job.Region, &job.UseCName, &job.ObjectKey, &job.Attempts, &job.MaxAttempts)
	return job, err
}

func (worker *OSSDeletionWorker) complete(ctx context.Context, job ossDeletionJob) error {
	tag, err := worker.server.db.Exec(ctx, `update oss_object_deletion_outbox set
		status='completed',deleted_at=now(),dead_at=null,locked_at=null,locked_by='',last_error='',failure_class='',updated_at=now()
		where id=$1 and status='processing' and locked_by=$2`, job.ID, job.LockToken)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errOSSDeletionLeaseLost
	}
	return nil
}

func (worker *OSSDeletionWorker) deleteObject(ctx context.Context, job ossDeletionJob) error {
	cfg := worker.server.ossConfigFromSettings(ctx)
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin OSS deletion lease guard: %w", err)
	}
	defer tx.Rollback(ctx)
	var ownedID int64
	err = tx.QueryRow(ctx, `select id from oss_object_deletion_outbox
		where id=$1 and status='processing' and locked_by=$2 for share`, job.ID, job.LockToken).Scan(&ownedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errOSSDeletionLeaseLost
	}
	if err != nil {
		return fmt.Errorf("lock OSS deletion lease: %w", err)
	}
	// Keep the current lease locked through the provider side effect. Fencing
	// only complete() cannot stop an expired worker deleting a reused object key.
	if job.FileID == nil {
		var registered bool
		if err := tx.QueryRow(ctx, `select exists(
			select 1 from oss_files where object_key=$1 and status<>'deleted')`, job.ObjectKey).Scan(&registered); err != nil {
			return fmt.Errorf("guard unregistered OSS object deletion: %w", err)
		}
		if registered {
			return nil
		}
	}
	if !cfg.Enabled || cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return errOSSDeletionCredentialsUnavailable
	}
	if job.Bucket != "" {
		cfg.Bucket = job.Bucket
	}
	if job.Endpoint != "" {
		cfg.Endpoint = job.Endpoint
	}
	if job.Region != "" {
		cfg.Region = job.Region
	}
	if cfg.Bucket == "" || cfg.Endpoint == "" || cfg.Region == "" {
		return errOSSDeletionTargetIncomplete
	}
	client := newOSSClient(cfg, cfg.Endpoint, job.UseCName || isCustomOSSEndpoint(cfg.Endpoint))
	_, err = client.DeleteObject(ctx, &aliyunoss.DeleteObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket),
		Key:    aliyunoss.Ptr(job.ObjectKey),
	})
	return err
}

type ossDeletionFailureDecision struct {
	class     string
	retryable bool
}

func classifyOSSDeletionFailure(err error) ossDeletionFailureDecision {
	if errors.Is(err, errOSSDeletionCredentialsUnavailable) || errors.Is(err, errOSSConfigurationUnavailable) {
		return ossDeletionFailureDecision{class: "configuration"}
	}
	if errors.Is(err, errOSSDeletionTargetIncomplete) {
		return ossDeletionFailureDecision{class: "invalid_target"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ossDeletionFailureDecision{class: "timeout", retryable: true}
	}
	var serviceErr *aliyunoss.ServiceError
	if errors.As(err, &serviceErr) {
		code := strings.ToLower(serviceErr.Code)
		switch serviceErr.StatusCode {
		case 400, 404:
			return ossDeletionFailureDecision{class: "invalid_target"}
		case 401:
			return ossDeletionFailureDecision{class: "authentication"}
		case 403:
			if strings.Contains(code, "accesskey") || strings.Contains(code, "signature") || strings.Contains(code, "token") {
				return ossDeletionFailureDecision{class: "authentication"}
			}
			return ossDeletionFailureDecision{class: "authorization"}
		case 408:
			return ossDeletionFailureDecision{class: "timeout", retryable: true}
		case 429:
			return ossDeletionFailureDecision{class: "rate_limit", retryable: true}
		}
		if serviceErr.StatusCode >= 500 {
			return ossDeletionFailureDecision{class: "remote_transient", retryable: true}
		}
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) {
		return ossDeletionFailureDecision{class: "network", retryable: true}
	}
	return ossDeletionFailureDecision{class: "unknown", retryable: true}
}

func (worker *OSSDeletionWorker) recordFailure(ctx context.Context, job ossDeletionJob, deletionErr error) (bool, error) {
	decision := classifyOSSDeletionFailure(deletionErr)
	dead := !decision.retryable || job.Attempts >= job.MaxAttempts
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	lastError := truncateOSSDeletionError(deletionErr)
	var tag pgconn.CommandTag
	if dead {
		tag, err = tx.Exec(ctx, `update oss_object_deletion_outbox set status='dead',dead_at=now(),
			next_attempt_at=now(),locked_at=null,locked_by='',last_error=$3,failure_class=$4,updated_at=now()
			where id=$1 and status='processing' and locked_by=$2`, job.ID, job.LockToken, lastError, decision.class)
	} else {
		nextAttempt := time.Now().Add(ossDeletionRetryDelay(job.Attempts))
		tag, err = tx.Exec(ctx, `update oss_object_deletion_outbox set status='pending',next_attempt_at=$3,
			locked_at=null,locked_by='',last_error=$4,failure_class=$5,updated_at=now()
			where id=$1 and status='processing' and locked_by=$2`, job.ID, job.LockToken, nextAttempt, lastError, decision.class)
	}
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, errOSSDeletionLeaseLost
	}
	if dead {
		if err = insertOSSDeletionDeadAlertTx(ctx, tx, job.ID, job.Attempts, job.MaxAttempts, decision.class, lastError); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return dead, nil
}

func (worker *OSSDeletionWorker) deadLetterExhaustedLeases(ctx context.Context) (int, error) {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `select id,attempts,max_attempts from oss_object_deletion_outbox
		where attempts>=max_attempts and (status='pending' or
			(status='processing' and locked_at<now()-make_interval(secs => $1)))
		order by updated_at,id for update skip locked limit $2`, int(ossDeletionLeaseTimeout/time.Second), ossDeletionBatchSize)
	if err != nil {
		return 0, err
	}
	type exhaustedJob struct {
		id                    int64
		attempts, maxAttempts int
	}
	jobs := make([]exhaustedJob, 0)
	for rows.Next() {
		var job exhaustedJob
		if err = rows.Scan(&job.id, &job.attempts, &job.maxAttempts); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, job)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	const lastError = "OSS deletion worker lease expired after the maximum number of attempts"
	for _, job := range jobs {
		tag, updateErr := tx.Exec(ctx, `update oss_object_deletion_outbox set status='dead',dead_at=now(),
			locked_at=null,locked_by='',last_error=$2,failure_class='worker_lost',updated_at=now()
			where id=$1 and status in ('pending','processing')`, job.id, lastError)
		if updateErr != nil {
			return 0, updateErr
		}
		if tag.RowsAffected() != 1 {
			return 0, errOSSDeletionLeaseLost
		}
		if err = insertOSSDeletionDeadAlertTx(ctx, tx, job.id, job.attempts, job.maxAttempts, "worker_lost", lastError); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(jobs), nil
}

func insertOSSDeletionDeadAlertTx(ctx context.Context, tx pgx.Tx, jobID int64, attempts, maxAttempts int, failureClass, lastError string) error {
	_, err := tx.Exec(ctx, `insert into app_logs(category,level,action,target,payload)
		values('infrastructure','error','oss_deletion_dead',$1::text,jsonb_build_object(
			'attempts',$2::integer,'maxAttempts',$3::integer,'failureClass',$4::text,'lastError',$5::text))`,
		fmt.Sprintf("%d", jobID), attempts, maxAttempts, failureClass, lastError)
	return err
}

func ossDeletionRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 12 {
		attempts = 12
	}
	delay := time.Second * time.Duration(1<<uint(attempts))
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func truncateOSSDeletionError(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	for len(value) > 2000 {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}

func (s *Server) tombstoneOSSFileTx(ctx context.Context, tx pgx.Tx, fileID int64, reason string) error {
	var target ossDeletionTarget
	var status string
	target.FileID = &fileID
	target.Reason = strings.TrimSpace(reason)
	err := tx.QueryRow(ctx, `select bucket,endpoint,region,object_key,status from oss_files where id=$1 for update`, fileID).
		Scan(&target.Bucket, &target.Endpoint, &target.Region, &target.ObjectKey, &status)
	if err != nil {
		return err
	}
	if target.ObjectKey == "" {
		return errors.New("OSS file object key is empty")
	}
	if target.Endpoint == "" || target.Bucket == "" || target.Region == "" {
		cfg := s.ossConfigFromSettingsWithQueryer(ctx, tx)
		if target.Endpoint == "" {
			target.Endpoint = cfg.Endpoint
		}
		if target.Bucket == "" {
			target.Bucket = cfg.Bucket
		}
		if target.Region == "" {
			target.Region = cfg.Region
		}
	}
	target.UseCName = isCustomOSSEndpoint(target.Endpoint)
	if status == "deleted" {
		return nil
	}
	if _, err = tx.Exec(ctx, `update oss_files set status='deleted',updated_at=now() where id=$1`, fileID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update log_shares set status='source_deleted',deleted_at=now()
		where source_file_id=$1 and deleted_at is null`, fileID); err != nil {
		return err
	}
	return enqueueOSSObjectDeletionTx(ctx, tx, target)
}

func (s *Server) enqueueUnregisteredOSSObjectDeletion(ctx context.Context, cfg ossConfigPayload, objectKey, reason string) error {
	objectKey = strings.TrimSpace(objectKey)
	if objectKey == "" {
		return nil
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin unregistered OSS object cleanup: %w", err)
	}
	defer tx.Rollback(ctx)
	var registered bool
	if err = tx.QueryRow(ctx, `select exists(
		select 1 from oss_files where object_key=$1 and status<>'deleted')`, objectKey).Scan(&registered); err != nil {
		return fmt.Errorf("guard unregistered OSS object cleanup: %w", err)
	}
	if registered {
		return nil
	}
	target := ossDeletionTarget{
		Bucket:    cfg.Bucket,
		Endpoint:  cfg.Endpoint,
		Region:    cfg.Region,
		UseCName:  cfg.UseCName || isCustomOSSEndpoint(cfg.Endpoint),
		ObjectKey: objectKey,
		Reason:    strings.TrimSpace(reason),
	}
	if err = enqueueOSSObjectDeletionTx(ctx, tx, target); err != nil {
		return fmt.Errorf("queue unregistered OSS object cleanup: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit unregistered OSS object cleanup: %w", err)
	}
	return nil
}

func enqueueOSSObjectDeletionTx(ctx context.Context, tx pgx.Tx, target ossDeletionTarget) error {
	target.Bucket = strings.TrimSpace(target.Bucket)
	target.Endpoint = strings.TrimSpace(target.Endpoint)
	target.Region = strings.TrimSpace(target.Region)
	target.ObjectKey = strings.TrimSpace(target.ObjectKey)
	if target.Bucket == "" || target.Endpoint == "" || target.Region == "" || target.ObjectKey == "" {
		return fmt.Errorf("OSS deletion target is incomplete")
	}
	_, err := tx.Exec(ctx, `insert into oss_object_deletion_outbox(
		oss_file_id,bucket,endpoint,region,use_cname,object_key,reason)
		values($1,$2,$3,$4,$5,$6,$7)
		on conflict(bucket,endpoint,object_key) do update set
			oss_file_id=case when oss_object_deletion_outbox.status in ('completed','dead')
				then coalesce(excluded.oss_file_id,oss_object_deletion_outbox.oss_file_id)
				else oss_object_deletion_outbox.oss_file_id end,
			region=case when oss_object_deletion_outbox.status in ('completed','dead')
				then excluded.region else oss_object_deletion_outbox.region end,
			use_cname=case when oss_object_deletion_outbox.status in ('completed','dead')
				then excluded.use_cname else oss_object_deletion_outbox.use_cname end,
			reason=excluded.reason,
			status=case when oss_object_deletion_outbox.status in ('completed','dead') then 'pending' else oss_object_deletion_outbox.status end,
			attempts=case when oss_object_deletion_outbox.status in ('completed','dead') then 0 else oss_object_deletion_outbox.attempts end,
			next_attempt_at=case when oss_object_deletion_outbox.status in ('completed','dead') then now() else oss_object_deletion_outbox.next_attempt_at end,
			locked_at=case when oss_object_deletion_outbox.status in ('completed','dead') then null else oss_object_deletion_outbox.locked_at end,
			locked_by=case when oss_object_deletion_outbox.status in ('completed','dead') then '' else oss_object_deletion_outbox.locked_by end,
			last_error=case when oss_object_deletion_outbox.status in ('completed','dead') then '' else oss_object_deletion_outbox.last_error end,
			failure_class=case when oss_object_deletion_outbox.status in ('completed','dead') then '' else oss_object_deletion_outbox.failure_class end,
			deleted_at=case when oss_object_deletion_outbox.status in ('completed','dead') then null else oss_object_deletion_outbox.deleted_at end,
			dead_at=case when oss_object_deletion_outbox.status in ('completed','dead') then null else oss_object_deletion_outbox.dead_at end,
			updated_at=now()`,
		target.FileID, target.Bucket, target.Endpoint, target.Region, target.UseCName, target.ObjectKey, target.Reason)
	return err
}
