package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/semaphore"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

const (
	maxBlueprintSourceBytes     = 32 << 20
	maxBlueprintProcessWorkers  = int64(2)
	blueprintHeartbeatInterval  = 30 * time.Second
	blueprintLeaseTTL           = 2 * time.Minute
	blueprintRecoveryInterval   = time.Minute
	blueprintRecoveryBatchLimit = 100
)

var blueprintProcessingSlots = semaphore.NewWeighted(maxBlueprintProcessWorkers)

var (
	errBlueprintJobLeaseActive = errors.New("blueprint job lease is still active")
	errBlueprintJobLeaseLost   = errors.New("blueprint job lease was lost")
)

type BlueprintWorker struct {
	cfg      config.Config
	db       *pgxpool.Pool
	queue    *queue.Client
	api      *Server
	workerID string
}

type blueprintJobMessage struct {
	JobID int64 `json:"jobId"`
}

func NewBlueprintWorker(cfg config.Config, db *pgxpool.Pool, queueClient *queue.Client) *BlueprintWorker {
	return &BlueprintWorker{
		cfg: cfg, db: db, queue: queueClient, api: &Server{cfg: cfg, db: db, queue: queueClient},
		workerID: "blueprint-worker-" + newExportID(),
	}
}

func (worker *BlueprintWorker) Start(ctx context.Context) error {
	if worker == nil || worker.db == nil {
		return queue.ErrUnavailable
	}
	_, _, staleRecoveryErr := worker.recoverStaleBlueprintJobs(ctx)
	_, artifactRecoveryErr := worker.recoverOrphanedBlueprintArtifacts(ctx)
	recoveryErr := errors.Join(staleRecoveryErr, artifactRecoveryErr)
	go worker.recoveryLoop(ctx)
	if worker.queue == nil {
		return errors.Join(recoveryErr, queue.ErrUnavailable)
	}
	subscribeErr := worker.queue.SubscribeTask("blueprint_convert", worker.handleMessage)
	if recoveryErr != nil {
		recoveryErr = fmt.Errorf("recover stale blueprint jobs: %w", recoveryErr)
	}
	return errors.Join(recoveryErr, subscribeErr)
}

func (worker *BlueprintWorker) recoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(blueprintRecoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, _, err := worker.recoverStaleBlueprintJobs(ctx); err != nil {
				slog.Error("recover stale blueprint jobs", "module", "blueprint", "error", err)
			}
			if _, err := worker.recoverOrphanedBlueprintArtifacts(ctx); err != nil {
				slog.Error("recover orphaned blueprint artifacts", "module", "blueprint", "error", err)
			}
		}
	}
}

func (worker *BlueprintWorker) handleMessage(ctx context.Context, raw []byte) error {
	var message blueprintJobMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		return err
	}
	if message.JobID <= 0 {
		return errors.New("blueprint job ID is missing")
	}
	return worker.processJob(ctx, message.JobID)
}

type claimedBlueprintJob struct {
	id, blueprintID, createdBy int64
	operation, targetFormat    string
	attempts, maxAttempts      int
	runToken                   string
}

func (worker *BlueprintWorker) processJob(ctx context.Context, jobID int64) error {
	if err := blueprintProcessingSlots.Acquire(ctx, 1); err != nil {
		return err
	}
	defer blueprintProcessingSlots.Release(1)
	job, claimed, err := worker.claimBlueprintJob(ctx, jobID)
	if err != nil || !claimed {
		return err
	}
	workContext, stopHeartbeat := worker.startBlueprintJobHeartbeat(ctx, job.id, job.runToken)
	var workErr error
	switch job.operation {
	case "normalize":
		workErr = worker.normalizeBlueprint(workContext, job.id, job.attempts, job.runToken, job.blueprintID, job.createdBy)
	case "convert":
		workErr = worker.convertBlueprint(workContext, job.id, job.attempts, job.runToken, job.blueprintID, job.createdBy, job.targetFormat)
	default:
		workErr = fmt.Errorf("unsupported blueprint operation %q", job.operation)
	}
	heartbeatCause := context.Cause(workContext)
	stopHeartbeat()
	if heartbeatCause != nil {
		workErr = errors.Join(workErr, heartbeatCause)
	}
	if workErr != nil {
		persistContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, persistErr := worker.failBlueprintJob(persistContext, job, workErr)
		cancel()
		if persistErr != nil {
			return errors.Join(workErr, persistErr)
		}
		return workErr
	}
	return nil
}

func (worker *BlueprintWorker) claimBlueprintJob(ctx context.Context, jobID int64) (claimedBlueprintJob, bool, error) {
	job := claimedBlueprintJob{id: jobID, runToken: worker.workerID + ":" + newExportID()}
	err := worker.db.QueryRow(ctx, `update blueprint_jobs set
		status='processing',progress=1,attempts=attempts+1,started_at=coalesce(started_at,now()),finished_at=null,
		locked_by=$2,lease_expires_at=now()+$3::interval,updated_at=now()
		where id=$1 and attempts<max_attempts and (
			status='queued' or (status='processing' and coalesce(lease_expires_at,updated_at)<=now())
		)
		returning blueprint_id,operation,target_format,coalesce(created_by,0),attempts,max_attempts`,
		jobID, job.runToken, pgInterval(blueprintLeaseTTL)).Scan(
		&job.blueprintID, &job.operation, &job.targetFormat, &job.createdBy, &job.attempts, &job.maxAttempts)
	if err == nil {
		return job, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return claimedBlueprintJob{}, false, err
	}
	var status, lastError string
	var attempts, maxAttempts int
	var leaseActive bool
	err = worker.db.QueryRow(ctx, `select status,last_error,attempts,max_attempts,coalesce(lease_expires_at>now(),false)
		from blueprint_jobs where id=$1`, jobID).
		Scan(&status, &lastError, &attempts, &maxAttempts, &leaseActive)
	if errors.Is(err, pgx.ErrNoRows) || status == "completed" {
		return claimedBlueprintJob{}, false, nil
	}
	if err != nil {
		return claimedBlueprintJob{}, false, err
	}
	if status == "processing" && leaseActive {
		return claimedBlueprintJob{}, false, errBlueprintJobLeaseActive
	}
	return claimedBlueprintJob{}, false, fmt.Errorf("blueprint job %d is not claimable: status=%s attempts=%d/%d: %s", jobID, status, attempts, maxAttempts, lastError)
}

func (worker *BlueprintWorker) startBlueprintJobHeartbeat(parent context.Context, jobID int64, runToken string) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(blueprintHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := worker.renewBlueprintJobLease(ctx, jobID, runToken); err != nil {
					cancel(err)
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

func (worker *BlueprintWorker) renewBlueprintJobLease(ctx context.Context, jobID int64, runToken string) error {
	tag, err := worker.db.Exec(ctx, `update blueprint_jobs set lease_expires_at=now()+$3::interval,updated_at=now()
		where id=$1 and status='processing' and locked_by=$2`, jobID, runToken, pgInterval(blueprintLeaseTTL))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errBlueprintJobLeaseLost
	}
	return nil
}

func (worker *BlueprintWorker) updateBlueprintJobProgress(ctx context.Context, jobID int64, runToken string, progress int) error {
	tag, err := worker.db.Exec(ctx, `update blueprint_jobs set progress=$3,lease_expires_at=now()+$4::interval,updated_at=now()
		where id=$1 and status='processing' and locked_by=$2`, jobID, runToken, progress, pgInterval(blueprintLeaseTTL))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errBlueprintJobLeaseLost
	}
	return nil
}

func (worker *BlueprintWorker) completeBlueprintJob(ctx context.Context, jobID int64, runToken string) error {
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = completeBlueprintJobTx(ctx, tx, jobID, runToken); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func completeBlueprintJobTx(ctx context.Context, tx pgx.Tx, jobID int64, runToken string) error {
	tag, err := tx.Exec(ctx, `update blueprint_jobs set status='completed',progress=100,finished_at=now(),updated_at=now(),
		last_error='',locked_by='',lease_expires_at=null where id=$1 and status='processing' and locked_by=$2`, jobID, runToken)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errBlueprintJobLeaseLost
	}
	return nil
}

func (worker *BlueprintWorker) failBlueprintJob(ctx context.Context, job claimedBlueprintJob, cause error) (bool, error) {
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	terminal := job.attempts >= job.maxAttempts || errors.Is(cause, errBlueprintEntityDataWouldBeLost)
	lastError := cause.Error()
	if len(lastError) > 2000 {
		lastError = lastError[:2000]
	}
	if err = abandonBlueprintJobArtifactsTx(ctx, tx, job.id, job.attempts, "blueprint-job-failed"); err != nil {
		return false, err
	}
	var tag pgconn.CommandTag
	if terminal {
		tag, err = tx.Exec(ctx, `update blueprint_jobs set status='failed',progress=0,finished_at=now(),updated_at=now(),
			last_error=$3,locked_by='',lease_expires_at=null where id=$1 and status='processing' and locked_by=$2`, job.id, job.runToken, lastError)
	} else {
		tag, err = tx.Exec(ctx, `update blueprint_jobs set status='queued',progress=0,finished_at=null,updated_at=now(),
			last_error=$3,locked_by='',lease_expires_at=null where id=$1 and status='processing' and locked_by=$2`, job.id, job.runToken, lastError)
	}
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, errBlueprintJobLeaseLost
	}
	if job.operation == "normalize" {
		blueprintStatus := "queued"
		if terminal {
			blueprintStatus = "failed"
		}
		if _, err = tx.Exec(ctx, `update blueprints set status=$2,last_error=$3,updated_at=now()
			where id=$1 and status in ('queued','processing')`, job.blueprintID, blueprintStatus, lastError); err != nil {
			return false, err
		}
	}
	if terminal {
		if err = worker.notifyTemplateTx(ctx, tx, job.createdBy, "blueprint_conversion_failure", map[string]string{"error": cause.Error()}, job.blueprintID); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return terminal, nil
}

func (worker *BlueprintWorker) normalizeBlueprint(ctx context.Context, jobID int64, attempt int, runToken string, blueprintID, createdBy int64) error {
	var publicID, objectKey, sourceFormat, title, description, coverKey string
	var coverFileID int64
	if err := worker.db.QueryRow(ctx, `update blueprints set status='processing',last_error='',updated_at=now()
		where id=$1 and status in ('queued','processing','failed')
		and exists(select 1 from blueprint_jobs job where job.id=$2 and job.blueprint_id=$1
			and job.status='processing' and job.attempts=$3 and job.locked_by=$4 for share)
		returning public_id,original_object_key,source_format,title,description_markdown,coalesce(cover_file_id,0),cover_object_key`, blueprintID, jobID, attempt, runToken).
		Scan(&publicID, &objectKey, &sourceFormat, &title, &description, &coverFileID, &coverKey); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errBlueprintJobLeaseLost
		}
		return err
	}
	raw, _, err := worker.readOSSObject(ctx, objectKey, maxBlueprintSourceBytes)
	if err != nil {
		return err
	}
	if err = worker.updateBlueprintJobProgress(ctx, jobID, runToken, 25); err != nil {
		return err
	}
	document, err := decodeBlueprint(raw, sourceFormat, title)
	if err != nil {
		return fmt.Errorf("decode %s blueprint: %w", sourceFormat, err)
	}
	materials, err := blueprintMaterials(document)
	if err != nil {
		return err
	}
	normalized, _, err := encodeBlueprint(document, "json")
	if err != nil {
		return err
	}
	if err = worker.updateBlueprintJobProgress(ctx, jobID, runToken, 55); err != nil {
		return err
	}
	normalizedConfig := worker.api.ossConfigFromSettings(ctx)
	normalizedKey := blueprintArtifactObjectKey(normalizedConfig.Prefix, ossBlueprintReleaseCategory(publicID, "normalized"), jobID, attempt, blueprintArtifactNormalized, "json")
	normalizedArtifact, err := worker.writePendingBlueprintArtifact(ctx, jobID, attempt, runToken, blueprintArtifactNormalized,
		normalizedKey, "blueprint.json", "application/json", normalized, createdBy, "blueprint_normalized")
	if err != nil {
		return err
	}
	var generatedCoverArtifact *blueprintArtifact
	if coverFileID <= 0 && coverKey == "" {
		cover, coverErr := renderBlueprintCover(document)
		if coverErr != nil {
			log.Printf("render generated cover for blueprint %s: %v", publicID, coverErr)
		} else {
			generatedConfig := worker.api.ossConfigFromSettings(ctx)
			generatedKey := blueprintArtifactObjectKey(generatedConfig.Prefix, ossBlueprintTextCategory(publicID, "cover"), jobID, attempt, blueprintArtifactCover, "png")
			artifact, writeErr := worker.writePendingBlueprintArtifact(ctx, jobID, attempt, runToken, blueprintArtifactCover,
				generatedKey, "blueprint-cover.png", "image/png", cover, createdBy, "blueprint_generated_cover")
			if writeErr != nil {
				if cleanupErr := worker.abandonBlueprintArtifact(ctx, artifact, "blueprint-generated-cover-upload-failed"); cleanupErr != nil {
					return errors.Join(writeErr, cleanupErr)
				}
				log.Printf("store generated cover for blueprint %s: %v", publicID, writeErr)
			} else {
				generatedCoverArtifact = &artifact
				coverFileID, coverKey = artifact.fileID, generatedKey
			}
		}
	}
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, "blueprint:"+publicID); err != nil {
		return fmt.Errorf("lock blueprint revision: %w", err)
	}
	var existingReviewStatus string
	var existingPublishedRevisionID *int64
	var currentCoverFileID, currentNormalizedFileID int64
	var currentCoverKey string
	if err = tx.QueryRow(ctx, `select review_status,published_revision_id,coalesce(cover_file_id,0),cover_object_key,
		coalesce(normalized_file_id,0) from blueprints where id=$1 for update`, blueprintID).
		Scan(&existingReviewStatus, &existingPublishedRevisionID, &currentCoverFileID, &currentCoverKey, &currentNormalizedFileID); err != nil {
		return err
	}
	if currentCoverFileID > 0 || currentCoverKey != "" {
		if generatedCoverArtifact != nil {
			if err = abandonBlueprintArtifactTx(ctx, tx, *generatedCoverArtifact, "blueprint-generated-cover-superseded"); err != nil {
				return err
			}
			generatedCoverArtifact = nil
		}
		coverFileID, coverKey = currentCoverFileID, currentCoverKey
	}
	if currentNormalizedFileID > 0 && currentNormalizedFileID != normalizedArtifact.fileID {
		return errors.New("blueprint already has a different normalized artifact")
	}
	if err = replaceBlueprintMaterialsTx(ctx, tx, blueprintID, materials); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `delete from blueprint_mods where blueprint_id=$1`, blueprintID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into blueprint_mods(blueprint_id,source_namespace,mod_id)
		select $1,namespace.source_namespace,source.mod_id
		from (
			select distinct split_part(block_id,':',1) source_namespace
			from blueprint_materials where blueprint_id=$1 and position(':' in block_id) > 1
		) namespace
		join lateral (
			select candidate.mod_id from (
				select revision.mod_id,0 priority,coalesce(revision.activated_at,revision.created_at) matched_at
				from catalog_import_revisions revision join mods public_source on public_source.id=revision.mod_id and public_source.review_status='approved'
				where revision.source_namespace=namespace.source_namespace
				  and revision.is_active and revision.status in ('ready','partial')
				union all
				select mod.id,1 priority,mod.updated_at matched_at from mods mod
				where mod.review_status='approved' and exists (
					select 1 from mod_identifiers identifier
					where identifier.mod_id=mod.id and lower(identifier.identifier)=lower(namespace.source_namespace)
				)
			) candidate order by candidate.priority,candidate.matched_at desc limit 1
		) source on true`, blueprintID); err != nil {
		return err
	}
	status := "ready"
	if len(document.Warnings) > 0 {
		status = "partial"
	}
	var revisionCount int
	if err = tx.QueryRow(ctx, `select count(*) from content_revisions where aggregate_type='blueprint' and aggregate_key=$1`, publicID).Scan(&revisionCount); err != nil {
		return err
	}
	reviewStatus := existingReviewStatus
	var publishedRevisionID any
	if existingPublishedRevisionID != nil {
		publishedRevisionID = *existingPublishedRevisionID
	}
	if revisionCount == 0 {
		reviewRequired := loadReviewConfig(ctx, tx).BlueprintCreate
		revisionStatus := "approved"
		reviewStatus = "approved"
		if reviewRequired {
			revisionStatus = "pending"
			reviewStatus = "pending"
		}
		var coverFilePublicID string
		if coverFileID > 0 {
			value, resolveErr := ossFilePublicIDForInternal(ctx, tx, &coverFileID)
			if resolveErr != nil {
				return resolveErr
			}
			if value != nil {
				coverFilePublicID = *value
			}
		}
		snapshot, _ := json.Marshal(blueprintContentSnapshot{PublicID: publicID, Title: title, Description: description, CoverFileID: coverFilePublicID, CoverKey: coverKey})
		created, createErr := createContentRevisionTx(ctx, tx, createContentRevisionParams{
			EntityType: "blueprint", EntityID: blueprintID,
			AggregateType: "blueprint", AggregateKey: publicID, Snapshot: snapshot, Reason: "New blueprint",
			ActorID: createdBy, Status: revisionStatus, Source: "blueprint_upload",
			Metadata: map[string]any{"blueprintId": publicID, "title": title, "operation": "create"},
		})
		if createErr != nil {
			return createErr
		}
		if !reviewRequired {
			publishedRevisionID = created.RevisionID
		} else {
			publishedRevisionID = nil
		}
	}
	if err = activateBlueprintArtifactTx(ctx, tx, normalizedArtifact); err != nil {
		return err
	}
	if generatedCoverArtifact != nil {
		if err = activateBlueprintArtifactTx(ctx, tx, *generatedCoverArtifact); err != nil {
			return err
		}
	}
	tag, err := tx.Exec(ctx, `update blueprints set status=$2,normalized_file_id=$3,normalized_object_key=$4,size_x=$5,size_y=$6,size_z=$7,
		block_count=$8,palette_count=$9,entity_count=$10,data_version=$11,review_status=$12,published_revision_id=$13,
		cover_file_id=nullif($14,0),cover_object_key=$15,last_error='',updated_at=now() where id=$1 and status='processing'`,
		blueprintID, status, normalizedArtifact.fileID, normalizedKey, document.Size[0], document.Size[1], document.Size[2], len(document.Blocks), len(materials), len(document.Entities), document.DataVersion, reviewStatus, publishedRevisionID, coverFileID, coverKey)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errBlueprintJobLeaseLost
	}
	if err = worker.notifyTemplateTx(ctx, tx, createdBy, "blueprint_conversion_success", map[string]string{"name": title}, blueprintID); err != nil {
		return err
	}
	if err = completeBlueprintJobTx(ctx, tx, jobID, runToken); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func replaceBlueprintMaterialsTx(ctx context.Context, tx pgx.Tx, blueprintID int64, materials []blueprintMaterial) error {
	if _, err := tx.Exec(ctx, `delete from blueprint_materials where blueprint_id=$1`, blueprintID); err != nil {
		return err
	}
	if len(materials) == 0 {
		return nil
	}
	copied, err := tx.CopyFrom(ctx, pgx.Identifier{"blueprint_materials"},
		[]string{"blueprint_id", "block_state", "block_id", "properties", "block_count"},
		pgx.CopyFromSlice(len(materials), func(index int) ([]any, error) {
			material := materials[index]
			propertyMap := material.Properties
			if propertyMap == nil {
				propertyMap = map[string]string{}
			}
			properties, marshalErr := json.Marshal(propertyMap)
			if marshalErr != nil {
				return nil, marshalErr
			}
			return []any{blueprintID, material.State, material.BlockID, string(properties), material.Count}, nil
		}))
	if err != nil {
		return err
	}
	if copied != int64(len(materials)) {
		return fmt.Errorf("copied %d of %d blueprint materials", copied, len(materials))
	}
	return nil
}

func (worker *BlueprintWorker) convertBlueprint(ctx context.Context, jobID int64, attempt int, runToken string, blueprintID, createdBy int64, targetFormat string) error {
	var publicID, title, normalizedKey string
	if err := worker.db.QueryRow(ctx, `select public_id,title,normalized_object_key from blueprints where id=$1`, blueprintID).Scan(&publicID, &title, &normalizedKey); err != nil {
		return err
	}
	if normalizedKey == "" {
		return errors.New("blueprint has not been normalized")
	}
	raw, _, err := worker.readOSSObject(ctx, normalizedKey, maxBlueprintNormalizedBytes)
	if err != nil {
		return err
	}
	var document blueprintDocument
	if err = json.Unmarshal(raw, &document); err != nil {
		return err
	}
	if err = worker.updateBlueprintJobProgress(ctx, jobID, runToken, 40); err != nil {
		return err
	}
	encoded, contentType, err := encodeBlueprint(document, targetFormat)
	if err != nil {
		return err
	}
	extension := strings.TrimPrefix(strings.ToLower(targetFormat), ".")
	conversionConfig := worker.api.ossConfigFromSettings(ctx)
	objectKey := blueprintArtifactObjectKey(conversionConfig.Prefix, ossBlueprintReleaseCategory(publicID, extension), jobID, attempt, blueprintArtifactConversion, extension)
	filename := strings.TrimSuffix(title, "."+document.SourceFormat) + "." + extension
	artifact, err := worker.writePendingBlueprintArtifact(ctx, jobID, attempt, runToken, blueprintArtifactConversion,
		objectKey, filename, contentType, encoded, createdBy, "blueprint_conversion")
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `insert into blueprint_variants(blueprint_id,format,file_id,object_key,original,recommended,status,original_name,content_type,size_bytes,sha256,created_by)
		values($1,$2,$3,$4,false,false,'ready',$5,$6,$7,$8,$9)`, blueprintID, extension, artifact.fileID, objectKey, filename, contentType, len(encoded), hex.EncodeToString(digest[:]), createdBy); err != nil {
		return err
	}
	if err = activateBlueprintArtifactTx(ctx, tx, artifact); err != nil {
		return err
	}
	if err = worker.notifyTemplateTx(ctx, tx, createdBy, "blueprint_format_success", map[string]string{"name": title, "format": strings.ToUpper(targetFormat)}, blueprintID); err != nil {
		return err
	}
	if err = completeBlueprintJobTx(ctx, tx, jobID, runToken); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type staleBlueprintJob struct {
	id, blueprintID, createdBy int64
	publicID, operation        string
	attempts, maxAttempts      int
}

func (worker *BlueprintWorker) recoverStaleBlueprintJobs(ctx context.Context) (int, int, error) {
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `select id,public_id,blueprint_id,operation,coalesce(created_by,0),attempts,max_attempts
		from blueprint_jobs where status='processing' and coalesce(lease_expires_at,updated_at)<=now()
		order by coalesce(lease_expires_at,updated_at),id for update skip locked limit $1`, blueprintRecoveryBatchLimit)
	if err != nil {
		return 0, 0, err
	}
	jobs := make([]staleBlueprintJob, 0)
	for rows.Next() {
		var job staleBlueprintJob
		if err = rows.Scan(&job.id, &job.publicID, &job.blueprintID, &job.operation, &job.createdBy, &job.attempts, &job.maxAttempts); err != nil {
			rows.Close()
			return 0, 0, err
		}
		jobs = append(jobs, job)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, 0, err
	}
	rows.Close()
	recovered := 0
	exhausted := make([]staleBlueprintJob, 0)
	for _, job := range jobs {
		if err = abandonBlueprintJobArtifactsTx(ctx, tx, job.id, job.attempts, "blueprint-job-lease-expired"); err != nil {
			return 0, 0, err
		}
		if job.attempts >= job.maxAttempts {
			const failure = "blueprint worker lease expired after the maximum number of attempts"
			tag, updateErr := tx.Exec(ctx, `update blueprint_jobs set status='failed',progress=0,last_error=$2,
				finished_at=now(),locked_by='',lease_expires_at=null,updated_at=now() where id=$1 and status='processing'`, job.id, failure)
			if updateErr != nil {
				return 0, 0, updateErr
			}
			if tag.RowsAffected() != 1 {
				return 0, 0, errBlueprintJobLeaseLost
			}
			if job.operation == "normalize" {
				if _, updateErr = tx.Exec(ctx, `update blueprints set status='failed',last_error=$2,updated_at=now()
					where id=$1 and status in ('queued','processing')`, job.blueprintID, failure); updateErr != nil {
					return 0, 0, updateErr
				}
			}
			if err = worker.notifyTemplateTx(ctx, tx, job.createdBy, "blueprint_conversion_failure", map[string]string{"error": "worker lease expired"}, job.blueprintID); err != nil {
				return 0, 0, err
			}
			exhausted = append(exhausted, job)
			continue
		}
		tag, updateErr := tx.Exec(ctx, `update blueprint_jobs set status='queued',progress=0,
			last_error='blueprint worker lease expired; queued for recovery',locked_by='',lease_expires_at=null,finished_at=null,updated_at=now()
			where id=$1 and status='processing'`, job.id)
		if updateErr != nil {
			return 0, 0, updateErr
		}
		if tag.RowsAffected() != 1 {
			return 0, 0, errBlueprintJobLeaseLost
		}
		if job.operation == "normalize" {
			if _, updateErr = tx.Exec(ctx, `update blueprints set status='queued',last_error='',updated_at=now()
				where id=$1 and status in ('queued','processing')`, job.blueprintID); updateErr != nil {
				return 0, 0, updateErr
			}
		}
		if _, updateErr = queue.EnqueueTx(ctx, tx, "blueprint_convert", "blueprint.conversion.recovered", "blueprint_job", job.publicID, "", blueprintJobMessage{JobID: job.id}); updateErr != nil {
			return 0, 0, updateErr
		}
		recovered++
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return recovered, len(exhausted), nil
}

func (worker *BlueprintWorker) readOSSObject(ctx context.Context, objectKey string, maximum int64) ([]byte, string, error) {
	client, cfg, err := worker.api.ossClient(ctx)
	if err != nil {
		return nil, "", err
	}
	result, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
	if err != nil {
		return nil, "", err
	}
	defer result.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(result.Body, maximum+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(raw)) > maximum {
		return nil, "", errors.New("blueprint exceeds processing size limit")
	}
	contentType := "application/octet-stream"
	if result.ContentType != nil {
		contentType = *result.ContentType
	}
	return raw, contentType, nil
}

func (worker *BlueprintWorker) notifyTemplateTx(ctx context.Context, tx pgx.Tx, userID int64, code string, values map[string]string, blueprintID int64) error {
	if userID <= 0 {
		return nil
	}
	var publicID, blueprintName string
	if err := tx.QueryRow(ctx, `select public_id,title from blueprints where id=$1`, blueprintID).Scan(&publicID, &blueprintName); err != nil {
		return err
	}
	if values == nil {
		values = map[string]string{}
	}
	if values["name"] == "" {
		values["name"] = blueprintName
	}
	targetData := map[string]any{"blueprintId": publicID, "targetLabel": blueprintName, "url": "/blueprints/" + publicID}
	return enqueueNotificationTaskTx(ctx, tx, "blueprint.notification."+code, "blueprint", publicID, "", notificationEvent{
		Action: "direct", RecipientID: userID, Kind: "system", TemplateKey: code, TemplateValues: values, Data: targetData,
	})
}
