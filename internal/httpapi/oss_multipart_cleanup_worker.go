package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

const (
	ossMultipartCleanupPollInterval = time.Minute
	ossMultipartCleanupLease        = 2 * time.Minute
	ossMultipartCleanupBatchSize    = 32
)

type ossMultipartCleanupJob struct {
	ID                       int64
	Attempts, MaxAttempts    int
	Bucket, Endpoint, Region string
	UseCName                 bool
	ObjectKey, UploadID      string
	LockToken                string
}

type OSSMultipartCleanupWorker struct {
	server   *Server
	workerID string
	abortFn  func(context.Context, ossMultipartCleanupJob) error
}

func NewOSSMultipartCleanupWorker(cfg config.Config, db *pgxpool.Pool) *OSSMultipartCleanupWorker {
	worker := &OSSMultipartCleanupWorker{server: &Server{cfg: cfg, db: db}, workerID: "oss-multipart-cleanup-" + newExportID()}
	worker.abortFn = worker.abortProviderUpload
	return worker
}

func (worker *OSSMultipartCleanupWorker) Start(ctx context.Context) error {
	if worker == nil || worker.server == nil || worker.server.db == nil {
		return nil
	}
	if err := worker.validateBucketLifecycle(ctx); err != nil {
		return err
	}
	go worker.run(ctx)
	return nil
}

func (worker *OSSMultipartCleanupWorker) validateBucketLifecycle(ctx context.Context) error {
	cfg := worker.server.ossConfigFromSettings(ctx)
	if !cfg.Enabled {
		return nil
	}
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" || cfg.Bucket == "" || cfg.Endpoint == "" || cfg.Region == "" {
		return errOSSConfigurationUnavailable
	}
	checkContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client := newOSSClient(cfg, cfg.Endpoint, cfg.UseCName || isCustomOSSEndpoint(cfg.Endpoint))
	result, err := client.GetBucketLifecycle(checkContext, &aliyunoss.GetBucketLifecycleRequest{Bucket: aliyunoss.Ptr(cfg.Bucket)})
	if err != nil {
		return fmt.Errorf("verify OSS multipart lifecycle: %w", err)
	}
	if result.LifecycleConfiguration == nil || !hasBoundedOSSMultipartLifecycle(result.LifecycleConfiguration.Rules, ossRoot(cfg.Prefix)) {
		return errors.New("OSS bucket requires an enabled unfiltered AbortMultipartUpload lifecycle rule of at most 7 days")
	}
	return nil
}

func hasBoundedOSSMultipartLifecycle(rules []aliyunoss.LifecycleRule, objectPrefix string) bool {
	objectPrefix = strings.TrimPrefix(strings.TrimSpace(objectPrefix), "/")
	for _, rule := range rules {
		if rule.Status == nil || !strings.EqualFold(*rule.Status, "Enabled") || rule.AbortMultipartUpload == nil ||
			rule.AbortMultipartUpload.Days == nil || *rule.AbortMultipartUpload.Days < 1 || *rule.AbortMultipartUpload.Days > 7 ||
			rule.Filter != nil || len(rule.Tags) != 0 {
			continue
		}
		prefix := ""
		if rule.Prefix != nil {
			prefix = strings.TrimPrefix(strings.TrimSpace(*rule.Prefix), "/")
		}
		if prefix == "" || strings.HasPrefix(objectPrefix, prefix) {
			return true
		}
	}
	return false
}

func (worker *OSSMultipartCleanupWorker) run(ctx context.Context) {
	ticker := time.NewTicker(ossMultipartCleanupPollInterval)
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

func (worker *OSSMultipartCleanupWorker) drain(ctx context.Context) {
	if _, err := worker.deadLetterExhausted(ctx); err != nil {
		log.Printf("dead-letter exhausted OSS multipart sessions: %v", err)
		return
	}
	for processed := 0; processed < ossMultipartCleanupBatchSize; processed++ {
		job, err := worker.claim(ctx)
		if errors.Is(err, pgx.ErrNoRows) || ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("claim OSS multipart cleanup: %v", err)
			return
		}
		if err = worker.abortFn(ctx, job); err != nil {
			if _, persistErr := worker.recordFailure(ctx, job, err); persistErr != nil {
				log.Printf("persist OSS multipart cleanup failure %d: %v", job.ID, persistErr)
			}
			continue
		}
		if err = worker.complete(ctx, job); err != nil {
			log.Printf("complete OSS multipart cleanup %d: %v", job.ID, err)
		}
	}
}

func (worker *OSSMultipartCleanupWorker) claim(ctx context.Context) (ossMultipartCleanupJob, error) {
	job := ossMultipartCleanupJob{LockToken: worker.workerID + ":" + newExportID()}
	err := worker.server.db.QueryRow(ctx, `update oss_multipart_sessions set status='aborting',attempts=attempts+1,
		locked_by=$2,lease_expires_at=now()+make_interval(secs => $1),updated_at=now()
		where id=(select id from oss_multipart_sessions where attempts<max_attempts and (
			(status='active' and expires_at<=now()) or
			(status='cleanup_pending' and next_attempt_at<=now()) or
			(status='aborting' and lease_expires_at<now()) or
			(status='completing' and expires_at<=now() and lease_expires_at<now()))
			order by coalesce(lease_expires_at,next_attempt_at,expires_at),id for update skip locked limit 1)
		returning id,bucket,endpoint,region,use_cname,object_key,upload_id,attempts,max_attempts`,
		int(ossMultipartCleanupLease/time.Second), job.LockToken).
		Scan(&job.ID, &job.Bucket, &job.Endpoint, &job.Region, &job.UseCName, &job.ObjectKey, &job.UploadID, &job.Attempts, &job.MaxAttempts)
	return job, err
}

func (worker *OSSMultipartCleanupWorker) abortProviderUpload(ctx context.Context, job ossMultipartCleanupJob) error {
	cfg := worker.server.ossConfigFromSettings(ctx)
	if !cfg.Enabled || cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return errOSSDeletionCredentialsUnavailable
	}
	cfg.Bucket, cfg.Endpoint, cfg.Region, cfg.UseCName = job.Bucket, job.Endpoint, job.Region, job.UseCName
	client := newOSSClient(cfg, cfg.Endpoint, cfg.UseCName || isCustomOSSEndpoint(cfg.Endpoint))
	return abortOSSMultipartUpload(ctx, client, cfg, job.ObjectKey, job.UploadID)
}

func (worker *OSSMultipartCleanupWorker) complete(ctx context.Context, job ossMultipartCleanupJob) error {
	tag, err := worker.server.db.Exec(ctx, `update oss_multipart_sessions set status='aborted',aborted_at=now(),
		locked_by='',lease_expires_at=null,last_error='',failure_class='',updated_at=now()
		where id=$1 and status='aborting' and locked_by=$2`, job.ID, job.LockToken)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errOSSMultipartSessionLeaseLost
	}
	return nil
}

func (worker *OSSMultipartCleanupWorker) recordFailure(ctx context.Context, job ossMultipartCleanupJob, abortErr error) (string, error) {
	decision := classifyOSSDeletionFailure(abortErr)
	dead := !decision.retryable || job.Attempts >= job.MaxAttempts
	state := "cleanup_pending"
	if dead {
		state = "dead"
	}
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var persisted string
	err = tx.QueryRow(ctx, `update oss_multipart_sessions set status=$3,next_attempt_at=$4,
		locked_by='',lease_expires_at=null,last_error=$5,failure_class=$6,
		dead_at=case when $3='dead' then now() else null end,updated_at=now()
		where id=$1 and status='aborting' and locked_by=$2 returning status`,
		job.ID, job.LockToken, state, time.Now().Add(ossDeletionRetryDelay(job.Attempts)),
		truncateOSSDeletionError(abortErr), decision.class).Scan(&persisted)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errOSSMultipartSessionLeaseLost
	}
	if err != nil {
		return "", err
	}
	if persisted == "dead" {
		if _, err = tx.Exec(ctx, `insert into app_logs(category,level,action,target,payload)
			values('infrastructure','error','oss_multipart_cleanup_dead',$1::text,jsonb_build_object(
				'attempts',$2::integer,'maxAttempts',$3::integer,'failureClass',$4::text,'lastError',$5::text))`,
			fmt.Sprintf("%d", job.ID), job.Attempts, job.MaxAttempts, decision.class, truncateOSSDeletionError(abortErr)); err != nil {
			return "", err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return persisted, nil
}

func (worker *OSSMultipartCleanupWorker) deadLetterExhausted(ctx context.Context) (int, error) {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `select id,attempts,max_attempts from oss_multipart_sessions
		where attempts>=max_attempts and (status in ('active','cleanup_pending') or
			(status in ('completing','aborting') and lease_expires_at<now()))
		order by updated_at,id for update skip locked limit $1`, ossMultipartCleanupBatchSize)
	if err != nil {
		return 0, err
	}
	type exhausted struct {
		id                    int64
		attempts, maxAttempts int
	}
	jobs := make([]exhausted, 0)
	for rows.Next() {
		var job exhausted
		if err = rows.Scan(&job.id, &job.attempts, &job.maxAttempts); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, job)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	const message = "OSS multipart cleanup lease expired after the maximum number of attempts"
	for _, job := range jobs {
		if _, err = tx.Exec(ctx, `update oss_multipart_sessions set status='dead',dead_at=now(),locked_by='',
			lease_expires_at=null,last_error=$2,failure_class='worker_lost',updated_at=now() where id=$1`, job.id, message); err != nil {
			return 0, err
		}
		if _, err = tx.Exec(ctx, `insert into app_logs(category,level,action,target,payload)
			values('infrastructure','error','oss_multipart_cleanup_dead',$1::text,jsonb_build_object(
				'attempts',$2::integer,'maxAttempts',$3::integer,'failureClass','worker_lost','lastError',$4::text))`,
			fmt.Sprintf("%d", job.id), job.attempts, job.maxAttempts, message); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(jobs), nil
}
