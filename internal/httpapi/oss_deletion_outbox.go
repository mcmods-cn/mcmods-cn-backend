package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

const (
	ossDeletionPollInterval = 15 * time.Second
	ossDeletionLeaseTimeout = 5 * time.Minute
	ossDeletionBatchSize    = 32
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
	ID        int64
	Bucket    string
	Endpoint  string
	Region    string
	UseCName  bool
	ObjectKey string
	Attempts  int
}

type OSSDeletionWorker struct {
	server *Server
}

func NewOSSDeletionWorker(cfg config.Config, db *pgxpool.Pool) *OSSDeletionWorker {
	return &OSSDeletionWorker{server: &Server{cfg: cfg, db: db}}
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
			worker.retry(ctx, job, err)
			continue
		}
		if _, err = worker.server.db.Exec(ctx, `update oss_object_deletion_outbox
			set status='completed',deleted_at=now(),locked_at=null,last_error='',updated_at=now() where id=$1`, job.ID); err != nil {
			log.Printf("complete OSS deletion outbox job %d: %v", job.ID, err)
		}
	}
}

func (worker *OSSDeletionWorker) claim(ctx context.Context) (ossDeletionJob, error) {
	var job ossDeletionJob
	err := worker.server.db.QueryRow(ctx, `update oss_object_deletion_outbox set
		status='processing',attempts=attempts+1,locked_at=now(),updated_at=now()
		where id=(select id from oss_object_deletion_outbox
			where (status='pending' and next_attempt_at<=now())
			   or (status='processing' and locked_at<now()-make_interval(secs => $1))
			order by next_attempt_at,id for update skip locked limit 1)
		returning id,bucket,endpoint,region,use_cname,object_key,attempts`, int(ossDeletionLeaseTimeout/time.Second)).
		Scan(&job.ID, &job.Bucket, &job.Endpoint, &job.Region, &job.UseCName, &job.ObjectKey, &job.Attempts)
	return job, err
}

func (worker *OSSDeletionWorker) deleteObject(ctx context.Context, job ossDeletionJob) error {
	cfg := worker.server.ossConfigFromSettings(ctx)
	if !cfg.Enabled || cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return errors.New("OSS credentials are unavailable")
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
		return errors.New("OSS deletion target is incomplete")
	}
	client := newOSSClient(cfg, cfg.Endpoint, job.UseCName || isCustomOSSEndpoint(cfg.Endpoint))
	_, err := client.DeleteObject(ctx, &aliyunoss.DeleteObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket),
		Key:    aliyunoss.Ptr(job.ObjectKey),
	})
	return err
}

func (worker *OSSDeletionWorker) retry(ctx context.Context, job ossDeletionJob, deletionErr error) {
	nextAttempt := time.Now().Add(ossDeletionRetryDelay(job.Attempts))
	if _, err := worker.server.db.Exec(ctx, `update oss_object_deletion_outbox set
		status='pending',next_attempt_at=$2,locked_at=null,last_error=$3,updated_at=now() where id=$1`,
		job.ID, nextAttempt, truncateOSSDeletionError(deletionErr)); err != nil {
		log.Printf("retry OSS deletion outbox job %d: %v", job.ID, err)
		return
	}
	log.Printf("OSS deletion outbox job %d failed (attempt %d); retry at %s: %v", job.ID, job.Attempts, nextAttempt.Format(time.RFC3339), deletionErr)
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
		cfg := s.ossConfigFromSettings(ctx)
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
	if status != "deleted" {
		if _, err = tx.Exec(ctx, `update oss_files set status='deleted',updated_at=now() where id=$1`, fileID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `update log_shares set status='source_deleted',deleted_at=now()
			where source_file_id=$1 and deleted_at is null`, fileID); err != nil {
			return err
		}
	}
	return enqueueOSSObjectDeletionTx(ctx, tx, target)
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
			oss_file_id=coalesce(oss_object_deletion_outbox.oss_file_id,excluded.oss_file_id),
			reason=excluded.reason,
			status=case when oss_object_deletion_outbox.status='completed' then 'completed' else 'pending' end,
			next_attempt_at=case when oss_object_deletion_outbox.status='completed' then oss_object_deletion_outbox.next_attempt_at else now() end,
			locked_at=null,last_error='',updated_at=now()`,
		target.FileID, target.Bucket, target.Endpoint, target.Region, target.UseCName, target.ObjectKey, target.Reason)
	return err
}
