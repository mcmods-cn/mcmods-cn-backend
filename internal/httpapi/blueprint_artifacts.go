package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

const (
	blueprintArtifactNormalized = "normalized"
	blueprintArtifactCover      = "cover"
	blueprintArtifactConversion = "conversion"
)

var errBlueprintArtifactState = errors.New("blueprint artifact state changed")

type blueprintArtifact struct {
	id        int64
	fileID    int64
	jobID     int64
	attempt   int
	role      string
	objectKey string
}

type pendingBlueprintArtifactTarget struct {
	artifactID int64
	fileID     int64
	status     string
	bucket     string
	endpoint   string
	region     string
	useCName   bool
	objectKey  string
}

// blueprintArtifactObjectKey is stable for one job attempt. A redelivery of
// the same attempt cannot create a second key, while a lease recovery gets a
// new attempt and therefore cannot overwrite the previous attempt's bytes.
func blueprintArtifactObjectKey(prefix, category string, jobID int64, attempt int, role, extension string) string {
	extension = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(extension), "."))
	for _, character := range extension {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			extension = "bin"
			break
		}
	}
	suffix := ""
	if extension != "" {
		suffix = "." + extension
	}
	return path.Join(
		ossObjectPrefix(prefix, category),
		fmt.Sprintf("job-%d-attempt-%d-%s%s", jobID, attempt, role, suffix),
	)
}

// writePendingBlueprintArtifact persists the ownership and deletion target
// before uploading. If the process exits at any later point, job lease
// recovery can enumerate the pending record and enqueue an idempotent delete.
func (worker *BlueprintWorker) writePendingBlueprintArtifact(
	ctx context.Context,
	jobID int64,
	attempt int,
	runToken string,
	role string,
	objectKey string,
	originalName string,
	contentType string,
	data []byte,
	uploaderID int64,
	source string,
) (blueprintArtifact, error) {
	client, cfg, err := worker.api.ossClient(ctx)
	if err != nil {
		return blueprintArtifact{}, err
	}
	digest := sha256.Sum256(data)
	sha := hex.EncodeToString(digest[:])
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return blueprintArtifact{}, err
	}
	defer tx.Rollback(ctx)
	var ownedJobID, blueprintID int64
	if err = tx.QueryRow(ctx, `select id,blueprint_id from blueprint_jobs
		where id=$1 and status='processing' and attempts=$2 and locked_by=$3 for update`, jobID, attempt, runToken).
		Scan(&ownedJobID, &blueprintID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return blueprintArtifact{}, errBlueprintJobLeaseLost
		}
		return blueprintArtifact{}, err
	}
	artifact := blueprintArtifact{jobID: jobID, attempt: attempt, role: role, objectKey: objectKey}
	err = tx.QueryRow(ctx, `insert into oss_files(
		bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
		content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status)
		values($1,$2,$3,$4,$5,$6,$7,$7,$8,$9,$9,$10,$11,'pending','trusted_generated') returning id`,
		cfg.Bucket, cfg.displayEndpoint(), cfg.Region, objectKey,
		ossCategoryFromObjectKey(objectKey, cfg.Prefix), source, originalName, contentType, len(data), sha, nullableUserID(uploaderID)).
		Scan(&artifact.fileID)
	if err != nil {
		return blueprintArtifact{}, err
	}
	err = tx.QueryRow(ctx, `insert into blueprint_job_artifacts(
		job_id,blueprint_id,attempt,role,file_id,bucket,endpoint,region,use_cname,object_key)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) returning id`,
		jobID, blueprintID, attempt, role, artifact.fileID, cfg.Bucket, cfg.Endpoint, cfg.Region,
		cfg.UseCName || isCustomOSSEndpoint(cfg.Endpoint), objectKey).Scan(&artifact.id)
	if err != nil {
		return blueprintArtifact{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return blueprintArtifact{}, err
	}
	_, err = client.PutObject(ctx, &aliyunoss.PutObjectRequest{
		Bucket:        aliyunoss.Ptr(cfg.Bucket),
		Key:           aliyunoss.Ptr(objectKey),
		ContentType:   aliyunoss.Ptr(contentType),
		ContentLength: aliyunoss.Ptr(int64(len(data))),
		Body:          bytes.NewReader(data),
		Metadata:      map[string]string{"sha256": sha},
	})
	if err != nil {
		return artifact, fmt.Errorf("upload pending blueprint %s artifact: %w", role, err)
	}
	return artifact, nil
}

func (worker *BlueprintWorker) abandonBlueprintArtifact(ctx context.Context, artifact blueprintArtifact, reason string) error {
	if artifact.id <= 0 {
		return nil
	}
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = abandonBlueprintArtifactTx(ctx, tx, artifact, reason); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func activateBlueprintArtifactTx(ctx context.Context, tx pgx.Tx, artifact blueprintArtifact) error {
	var fileID int64
	err := tx.QueryRow(ctx, `update blueprint_job_artifacts set
		status='active',activated_at=now(),updated_at=now()
		where id=$1 and job_id=$2 and attempt=$3 and role=$4 and status='pending'
		returning file_id`, artifact.id, artifact.jobID, artifact.attempt, artifact.role).Scan(&fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errBlueprintArtifactState
	}
	if err != nil {
		return err
	}
	if fileID != artifact.fileID {
		return errBlueprintArtifactState
	}
	tag, err := tx.Exec(ctx, `update oss_files set status='active',updated_at=now()
		where id=$1 and status='pending'`, fileID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errBlueprintArtifactState
	}
	return nil
}

func abandonBlueprintArtifactTx(ctx context.Context, tx pgx.Tx, artifact blueprintArtifact, reason string) error {
	var target pendingBlueprintArtifactTarget
	err := tx.QueryRow(ctx, `select id,file_id,bucket,endpoint,region,use_cname,object_key
		from blueprint_job_artifacts
		where id=$1 and job_id=$2 and attempt=$3 and role=$4 and status='pending' for update`,
		artifact.id, artifact.jobID, artifact.attempt, artifact.role).
		Scan(&target.artifactID, &target.fileID, &target.bucket, &target.endpoint, &target.region, &target.useCName, &target.objectKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return errBlueprintArtifactState
	}
	if err != nil {
		return err
	}
	target.status = "pending"
	return abandonPendingBlueprintArtifactTargetTx(ctx, tx, target, reason)
}

func abandonBlueprintJobArtifactsTx(ctx context.Context, tx pgx.Tx, jobID int64, attempt int, reason string) error {
	rows, err := tx.Query(ctx, `select id,file_id,bucket,endpoint,region,use_cname,object_key
		from blueprint_job_artifacts where job_id=$1 and attempt=$2 and status='pending'
		order by id for update`, jobID, attempt)
	if err != nil {
		return err
	}
	targets := make([]pendingBlueprintArtifactTarget, 0)
	for rows.Next() {
		var target pendingBlueprintArtifactTarget
		if err = rows.Scan(&target.artifactID, &target.fileID, &target.bucket, &target.endpoint, &target.region, &target.useCName, &target.objectKey); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, target)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, target := range targets {
		target.status = "pending"
		if err = abandonPendingBlueprintArtifactTargetTx(ctx, tx, target, reason); err != nil {
			return err
		}
	}
	return nil
}

func abandonPendingBlueprintArtifactTargetTx(ctx context.Context, tx pgx.Tx, target pendingBlueprintArtifactTarget, reason string) error {
	if target.status == "" {
		target.status = "pending"
	}
	tag, err := tx.Exec(ctx, `update oss_files set status='deleted',updated_at=now()
		where id=$1 and status=$2`, target.fileID, target.status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errBlueprintArtifactState
	}
	fileID := target.fileID
	if err = enqueueOSSObjectDeletionTx(ctx, tx, ossDeletionTarget{
		FileID: &fileID, Bucket: target.bucket, Endpoint: target.endpoint, Region: target.region,
		UseCName: target.useCName, ObjectKey: target.objectKey, Reason: strings.TrimSpace(reason),
	}); err != nil {
		return err
	}
	tag, err = tx.Exec(ctx, `update blueprint_job_artifacts set
		status='abandoned',abandoned_at=now(),updated_at=now() where id=$1 and status=$2`, target.artifactID, target.status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errBlueprintArtifactState
	}
	return nil
}

// recoverOrphanedBlueprintArtifacts is the safety net for administrative or
// cascading deletion of a job row. Artifact lineage survives those deletes;
// any pending record without its exact processing attempt is compensated.
func (worker *BlueprintWorker) recoverOrphanedBlueprintArtifacts(ctx context.Context) (int, error) {
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `select artifact.id,artifact.file_id,artifact.status,artifact.bucket,artifact.endpoint,
		artifact.region,artifact.use_cname,artifact.object_key
		from blueprint_job_artifacts artifact
		left join blueprint_jobs job on job.id=artifact.job_id
		where (artifact.status='pending'
		       and (job.id is null or job.status<>'processing' or job.attempts<>artifact.attempt))
		   or (artifact.status='active' and artifact.blueprint_id is null)
		order by artifact.id for update of artifact skip locked limit $1`, blueprintRecoveryBatchLimit)
	if err != nil {
		return 0, err
	}
	targets := make([]pendingBlueprintArtifactTarget, 0)
	for rows.Next() {
		var target pendingBlueprintArtifactTarget
		if err = rows.Scan(&target.artifactID, &target.fileID, &target.status, &target.bucket, &target.endpoint, &target.region, &target.useCName, &target.objectKey); err != nil {
			rows.Close()
			return 0, err
		}
		targets = append(targets, target)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, target := range targets {
		if err = abandonPendingBlueprintArtifactTargetTx(ctx, tx, target, "blueprint-job-artifact-orphaned"); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(targets), nil
}
