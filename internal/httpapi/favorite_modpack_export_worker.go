package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

type FavoriteModpackExportWorker struct {
	server *Server
	queue  *queue.Client
}

const favoriteExportMaxBuildAttempts = 3

type favoriteExportResultCounts struct {
	CollectionItems  int
	Exported         int
	AutoDependencies int
	Skipped          int
	Failed           int
	FinalFiles       int
}

func validateFavoriteExportCompletionCounts(task, report favoriteExportResultCounts, indexFileCount int) error {
	validate := func(label string, counts favoriteExportResultCounts) error {
		if counts.CollectionItems < 0 || counts.Exported < 0 || counts.AutoDependencies < 0 ||
			counts.Skipped < 0 || counts.Failed < 0 || counts.FinalFiles < 0 {
			return fmt.Errorf("%s contains a negative value", label)
		}
		if counts.CollectionItems != counts.Exported+counts.Skipped+counts.Failed {
			return fmt.Errorf("%s collection count does not match exported, skipped, and failed counts", label)
		}
		if counts.FinalFiles != counts.Exported+counts.AutoDependencies {
			return fmt.Errorf("%s final count does not match exported and dependency counts", label)
		}
		return nil
	}
	if err := validate("task", task); err != nil {
		return fmt.Errorf("favorite export result counts are inconsistent: %w", err)
	}
	if err := validate("report", report); err != nil {
		return fmt.Errorf("favorite export result counts are inconsistent: %w", err)
	}
	if task != report {
		return fmt.Errorf("favorite export result counts are inconsistent: task=%+v report=%+v", task, report)
	}
	if indexFileCount != task.FinalFiles {
		return fmt.Errorf("favorite export result counts are inconsistent: task final=%d index files=%d", task.FinalFiles, indexFileCount)
	}
	return nil
}

func favoriteExportMaxAttempts(value int) int {
	if value <= 0 {
		return favoriteExportMaxBuildAttempts
	}
	return value
}

func favoriteExportLeaseTTL(value time.Duration) time.Duration {
	if value <= 0 {
		return 15 * time.Minute
	}
	return value
}

func favoriteExportArtifactTTL(value time.Duration) time.Duration {
	if value <= 0 {
		return 7 * 24 * time.Hour
	}
	return value
}

func favoriteExportOrphanGracePeriod(leaseTTL time.Duration) time.Duration {
	return 2 * favoriteExportLeaseTTL(leaseTTL)
}

func NewFavoriteModpackExportWorker(cfg config.Config, db *pgxpool.Pool, queueClient *queue.Client) *FavoriteModpackExportWorker {
	return &FavoriteModpackExportWorker{server: &Server{cfg: cfg, db: db, queue: queueClient}, queue: queueClient}
}

func (worker *FavoriteModpackExportWorker) Start(ctx context.Context) error {
	if worker == nil || worker.server == nil {
		return queue.ErrUnavailable
	}
	subscribeErr := queue.ErrUnavailable
	if worker.queue != nil {
		subscribeErr = worker.queue.SubscribeTask(favoriteModpackExportTaskCode, worker.handle)
	}
	go worker.scan(ctx)
	if worker.server.cfg.NATS.OutboxEnabled {
		return nil
	}
	return subscribeErr
}

func (worker *FavoriteModpackExportWorker) scan(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	if err := worker.processPending(ctx); err != nil {
		slog.Error("scan favorite modpack exports", "error", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := worker.processPending(ctx); err != nil {
				slog.Error("scan favorite modpack exports", "error", err)
			}
		}
	}
}

func (worker *FavoriteModpackExportWorker) processPending(ctx context.Context) error {
	maxAttempts := favoriteExportMaxAttempts(worker.server.cfg.FavoriteExport.MaxBuildAttempts)
	var scanErrors []error
	if err := worker.recoverOrphanedArtifacts(ctx); err != nil {
		scanErrors = append(scanErrors, fmt.Errorf("recover orphaned favorite export artifacts: %w", err))
	}
	if err := worker.expireCompleted(ctx); err != nil {
		scanErrors = append(scanErrors, fmt.Errorf("expire completed exports: %w", err))
	}
	if err := worker.failExhaustedLeases(ctx); err != nil {
		scanErrors = append(scanErrors, fmt.Errorf("fail exhausted export leases: %w", err))
	}
	rows, err := worker.server.db.Query(ctx, `select public_id from favorite_modpack_export_tasks
		where (status='pending' and coalesce(lease_expires_at,now())<=now())
		   or (status='processing' and lease_expires_at<now() and attempt_count<$1)
		order by created_at limit 10`, maxAttempts)
	if err != nil {
		return errors.Join(append(scanErrors, fmt.Errorf("query pending favorite exports: %w", err))...)
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return errors.Join(append(scanErrors, fmt.Errorf("scan pending favorite export: %w", err))...)
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return errors.Join(append(scanErrors, fmt.Errorf("iterate pending favorite exports: %w", err))...)
	}
	rows.Close()
	for _, id := range ids {
		if err = worker.process(ctx, id); err != nil {
			scanErrors = append(scanErrors, fmt.Errorf("process favorite export %s: %w", id, err))
		}
	}
	return errors.Join(scanErrors...)
}

// recoverOrphanedArtifacts is the crash-recovery half of the export commit
// protocol. Immediate compensation handles an observed completion failure;
// this bounded scan handles a process dying after OSS registration or the
// compensation transaction itself being temporarily unavailable.
func (worker *FavoriteModpackExportWorker) recoverOrphanedArtifacts(ctx context.Context) error {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin favorite export artifact recovery: %w", err)
	}
	defer tx.Rollback(ctx)
	gracePeriod := favoriteExportOrphanGracePeriod(worker.server.cfg.FavoriteExport.LeaseTTL)
	rows, err := tx.Query(ctx, `select file.id from oss_files file
		where file.source='favorite_modpack_export' and file.status='active'
		  and file.created_at<=now()-$1::interval
		  and not exists(select 1 from favorite_modpack_export_tasks task where task.result_file_id=file.id)
		order by file.created_at,file.id for update of file skip locked limit 50`, gracePeriod.String())
	if err != nil {
		return fmt.Errorf("query orphaned favorite export artifacts: %w", err)
	}
	fileIDs := make([]int64, 0, 50)
	for rows.Next() {
		var fileID int64
		if err = rows.Scan(&fileID); err != nil {
			rows.Close()
			return fmt.Errorf("scan orphaned favorite export artifact: %w", err)
		}
		fileIDs = append(fileIDs, fileID)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate orphaned favorite export artifacts: %w", err)
	}
	rows.Close()
	for _, fileID := range fileIDs {
		if err = worker.server.tombstoneOSSFileTx(ctx, tx, fileID, "favorite_modpack_export_orphaned"); err != nil {
			return fmt.Errorf("tombstone orphaned favorite export artifact %d: %w", fileID, err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit favorite export artifact recovery: %w", err)
	}
	return nil
}

func (worker *FavoriteModpackExportWorker) failExhaustedLeases(ctx context.Context) error {
	maxAttempts := favoriteExportMaxAttempts(worker.server.cfg.FavoriteExport.MaxBuildAttempts)
	rows, err := worker.server.db.Query(ctx, `update favorite_modpack_export_tasks set status='failed',stage='failed',
		error_code='WORKER_LEASE_EXHAUSTED',error_detail='export processing failed',lease_token='',lease_expires_at=null,
		finished_at=now(),updated_at=now()
		where id in (select id from favorite_modpack_export_tasks where status='processing' and lease_expires_at<now()
			and attempt_count>=$1 order by lease_expires_at,id for update skip locked limit 20)
		returning public_id,owner_user_id,pack_name`, maxAttempts)
	if err != nil {
		return fmt.Errorf("claim exhausted favorite export leases: %w", err)
	}
	type exhaustedExport struct {
		taskID, name string
		ownerID      int64
	}
	exports := make([]exhaustedExport, 0)
	for rows.Next() {
		var item exhaustedExport
		if err = rows.Scan(&item.taskID, &item.ownerID, &item.name); err != nil {
			rows.Close()
			return fmt.Errorf("scan exhausted favorite export lease: %w", err)
		}
		exports = append(exports, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate exhausted favorite export leases: %w", err)
	}
	rows.Close()
	var notificationErrors []error
	for _, item := range exports {
		if err = worker.server.sendTemplatedNotification(ctx, item.ownerID, "modpack_export_failed", map[string]string{"pack_name": item.name, "stage": "processing", "reason": "WORKER_LEASE_EXHAUSTED"}, map[string]any{"url": "/account/favorites/exports/" + item.taskID, "taskId": item.taskID}); err != nil {
			notificationErrors = append(notificationErrors, fmt.Errorf("notify exhausted favorite export %s: %w", item.taskID, err))
		}
	}
	return errors.Join(notificationErrors...)
}

// expireCompleted removes only the generated temporary artifact. The task and
// its structured report stay in PostgreSQL so users can inspect or repeat an
// old export after its download has expired.
func (worker *FavoriteModpackExportWorker) expireCompleted(ctx context.Context) error {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin favorite export expiration: %w", err)
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `select task.id,file.id,task.owner_user_id,task.pack_name,task.public_id from favorite_modpack_export_tasks task
		join oss_files file on file.id=task.result_file_id
		where task.status='ready' and task.expires_at<=now()
		order by task.expires_at,task.id for update of task skip locked limit 50`)
	if err != nil {
		return fmt.Errorf("query expired favorite exports: %w", err)
	}
	type expiredArtifact struct {
		taskID, fileID, ownerID int64
		name, publicID          string
	}
	artifacts := make([]expiredArtifact, 0)
	for rows.Next() {
		var artifact expiredArtifact
		if err = rows.Scan(&artifact.taskID, &artifact.fileID, &artifact.ownerID, &artifact.name, &artifact.publicID); err != nil {
			rows.Close()
			return fmt.Errorf("scan expired favorite export: %w", err)
		}
		artifacts = append(artifacts, artifact)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate expired favorite exports: %w", err)
	}
	rows.Close()
	for _, artifact := range artifacts {
		if err = worker.server.tombstoneOSSFileTx(ctx, tx, artifact.fileID, "favorite_modpack_export_expired"); err != nil {
			return fmt.Errorf("tombstone expired favorite export %s: %w", artifact.publicID, err)
		}
		tag, updateErr := tx.Exec(ctx, `update favorite_modpack_export_tasks set status='expired',stage='expired',updated_at=now() where id=$1 and status='ready'`, artifact.taskID)
		if updateErr != nil {
			return fmt.Errorf("mark favorite export %s expired: %w", artifact.publicID, updateErr)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("favorite export %s lost its ready state during expiration", artifact.publicID)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit favorite export expiration: %w", err)
	}
	var notificationErrors []error
	for _, artifact := range artifacts {
		if err = worker.server.sendTemplatedNotification(ctx, artifact.ownerID, "modpack_export_expired", map[string]string{"pack_name": artifact.name}, map[string]any{"taskId": artifact.publicID, "url": "/user?section=favorites&exportTask=" + artifact.publicID}); err != nil {
			notificationErrors = append(notificationErrors, fmt.Errorf("notify expired favorite export %s: %w", artifact.publicID, err))
		}
	}
	return errors.Join(notificationErrors...)
}

func (worker *FavoriteModpackExportWorker) handle(ctx context.Context, raw []byte) error {
	var message favoriteModpackExportMessage
	if json.Unmarshal(raw, &message) != nil || message.TaskID == "" {
		return errors.New("invalid favorite export message")
	}
	return worker.process(ctx, message.TaskID)
}

func (worker *FavoriteModpackExportWorker) process(ctx context.Context, taskID string) error {
	var ownerID int64
	var name, version, minecraftVersion, loader, loaderVersion string
	var taskCounts favoriteExportResultCounts
	maxAttempts := favoriteExportMaxAttempts(worker.server.cfg.FavoriteExport.MaxBuildAttempts)
	leaseTTL := favoriteExportLeaseTTL(worker.server.cfg.FavoriteExport.LeaseTTL)
	leaseToken := newExportID()
	tag, err := worker.server.db.Exec(ctx, `update favorite_modpack_export_tasks set status='processing',stage='building',
		attempt_count=attempt_count+1,lease_token=$2,lease_expires_at=now()+$4::interval,
		started_at=coalesce(started_at,now()),updated_at=now()
		where public_id=$1 and attempt_count<$3 and (
			(status='pending' and coalesce(lease_expires_at,now())<=now()) or
			(status='processing' and lease_expires_at<now()))`, taskID, leaseToken, maxAttempts, leaseTTL.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	var attempt int
	err = worker.server.db.QueryRow(ctx, `select owner_user_id,pack_name,pack_version_id,minecraft_version,loader_type,loader_version,attempt_count,
		collection_item_count,exported_mod_count,auto_dependency_count,skipped_item_count,failed_item_count,final_file_count
		from favorite_modpack_export_tasks where public_id=$1 and lease_token=$2`, taskID, leaseToken).
		Scan(&ownerID, &name, &version, &minecraftVersion, &loader, &loaderVersion, &attempt,
			&taskCounts.CollectionItems, &taskCounts.Exported, &taskCounts.AutoDependencies,
			&taskCounts.Skipped, &taskCounts.Failed, &taskCounts.FinalFiles)
	if err != nil {
		return worker.fail(ctx, taskID, leaseToken, "TASK_LOAD_FAILED", err)
	}
	if attempt > maxAttempts {
		return worker.fail(ctx, taskID, leaseToken, "RETRY_LIMIT_REACHED", errors.New("export worker retry limit reached"))
	}
	rows, err := worker.server.db.Query(ctx, `select result_type,selected_file_name,sha1,sha512,env_client,env_server,download_url,file_size
		from favorite_modpack_export_items where task_id=(select id from favorite_modpack_export_tasks where public_id=$1) order by id`, taskID)
	if err != nil {
		return worker.fail(ctx, taskID, leaseToken, "REPORT_LOAD_FAILED", err)
	}
	files := make([]mrpackFile, 0)
	var reportCounts favoriteExportResultCounts
	for rows.Next() {
		var resultType, filename, sha1, sha512, clientEnv, serverEnv, download string
		var size int64
		if err = rows.Scan(&resultType, &filename, &sha1, &sha512, &clientEnv, &serverEnv, &download, &size); err != nil {
			rows.Close()
			return worker.fail(ctx, taskID, leaseToken, "REPORT_LOAD_FAILED", fmt.Errorf("scan export report item: %w", err))
		}
		switch resultType {
		case "exported":
			reportCounts.CollectionItems++
			reportCounts.Exported++
		case "auto_dependency":
			reportCounts.AutoDependencies++
		case "skipped":
			reportCounts.CollectionItems++
			reportCounts.Skipped++
			continue
		case "failed":
			reportCounts.CollectionItems++
			reportCounts.Failed++
			continue
		default:
			rows.Close()
			return worker.fail(ctx, taskID, leaseToken, "REPORT_INCONSISTENT", fmt.Errorf("favorite export result counts are inconsistent: unknown report result type %q", resultType))
		}
		reportCounts.FinalFiles++
		files = append(files, mrpackFile{Path: "mods/" + filename, Hashes: map[string]string{"sha1": sha1, "sha512": sha512}, Env: mrpackEnvironment{Client: clientEnv, Server: serverEnv}, Downloads: []string{download}, FileSize: size})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return worker.fail(ctx, taskID, leaseToken, "REPORT_LOAD_FAILED", fmt.Errorf("iterate export report items: %w", err))
	}
	rows.Close()
	if err = validateFavoriteExportCompletionCounts(taskCounts, reportCounts, reportCounts.FinalFiles); err != nil {
		return worker.fail(ctx, taskID, leaseToken, "REPORT_INCONSISTENT", err)
	}
	result, err := buildMRPack(mrpackBuildInput{Name: name, VersionID: version, Summary: "Exported from an MCMods favorite collection", MinecraftVersion: minecraftVersion, Loader: loader, LoaderVersion: loaderVersion, Files: files})
	if err != nil {
		return worker.fail(ctx, taskID, leaseToken, "MRPACK_BUILD_FAILED", err)
	}
	if err = validateFavoriteExportCompletionCounts(taskCounts, reportCounts, result.IndexFileCount); err != nil {
		return worker.fail(ctx, taskID, leaseToken, "REPORT_INCONSISTENT", err)
	}
	cfg := worker.server.ossConfigFromSettings(ctx)
	objectKey := path.Join(ossObjectPrefix(cfg.Prefix, "temporary/modpack-exports/"+taskID), randomObjectName()+".mrpack")
	filename := safeMRPackDownloadName(name, minecraftVersion, loader)
	fileID, err := worker.server.writeGeneratedOSSObject(ctx, objectKey, filename, "application/x-modrinth-modpack+zip", result.Data, ownerID, "favorite_modpack_export")
	if err != nil {
		return worker.fail(ctx, taskID, leaseToken, "OSS_UPLOAD_FAILED", err)
	}
	expires := time.Now().Add(favoriteExportArtifactTTL(worker.server.cfg.FavoriteExport.ArtifactTTL))
	completed, err := worker.finalizeFavoriteExportArtifact(ctx, taskID, leaseToken, fileID, result.Size, result.SHA256, expires)
	if err != nil {
		compensationErr := worker.compensateUnlinkedFavoriteExportArtifact(ctx, taskID, fileID)
		return errors.Join(err, compensationErr)
	}
	if !completed {
		return nil
	}
	template := "modpack_export_completed"
	skipped := taskCounts.Skipped + taskCounts.Failed
	if skipped > 0 {
		template = "modpack_export_completed_with_skips"
	}
	if err = worker.server.sendTemplatedNotification(ctx, ownerID, template, map[string]string{"pack_name": name, "minecraft_version": minecraftVersion, "loader": strings.ToUpper(loader) + " " + loaderVersion, "exported": fmt.Sprint(taskCounts.Exported), "dependencies": fmt.Sprint(taskCounts.AutoDependencies), "skipped": fmt.Sprint(skipped)}, map[string]any{"url": "/user?section=favorites&exportTask=" + taskID, "taskId": taskID}); err != nil {
		return fmt.Errorf("notify completed favorite export %s: %w", taskID, err)
	}
	return nil
}

// compensateUnlinkedFavoriteExportArtifact serializes with every completion
// path through the task row. If Commit returned an ambiguous error but the
// ready state is already durable, the linked artifact is preserved.
func (worker *FavoriteModpackExportWorker) compensateUnlinkedFavoriteExportArtifact(ctx context.Context, taskID string, fileID int64) error {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin favorite export artifact compensation: %w", err)
	}
	defer tx.Rollback(ctx)
	var linkedFileID *int64
	err = tx.QueryRow(ctx, `select result_file_id from favorite_modpack_export_tasks where public_id=$1 for update`, taskID).
		Scan(&linkedFileID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock favorite export artifact compensation: %w", err)
	}
	if linkedFileID != nil && *linkedFileID == fileID {
		if err = tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit linked favorite export artifact guard: %w", err)
		}
		return nil
	}
	var referenced bool
	if err = tx.QueryRow(ctx, `select exists(select 1 from favorite_modpack_export_tasks where result_file_id=$1)`, fileID).Scan(&referenced); err != nil {
		return fmt.Errorf("guard favorite export artifact compensation: %w", err)
	}
	if !referenced {
		if err = worker.server.tombstoneOSSFileTx(ctx, tx, fileID, "favorite_modpack_export_finalize_failed"); err != nil {
			return fmt.Errorf("tombstone unlinked favorite export artifact: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit favorite export artifact compensation: %w", err)
	}
	return nil
}

func (worker *FavoriteModpackExportWorker) finalizeFavoriteExportArtifact(ctx context.Context, taskID, leaseToken string, fileID, fileSize int64, sha256 string, expires time.Time) (bool, error) {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin favorite export completion: %w", err)
	}
	defer tx.Rollback(ctx)
	var status, currentLease string
	if err = tx.QueryRow(ctx, `select status,lease_token from favorite_modpack_export_tasks where public_id=$1 for update`, taskID).
		Scan(&status, &currentLease); err != nil {
		return false, fmt.Errorf("lock favorite export completion: %w", err)
	}
	if status == "cancelled" {
		if err = worker.cleanupCancelledFavoriteExportArtifact(ctx, tx, taskID, fileID, fileSize, sha256); err != nil {
			return false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit cancelled favorite export cleanup: %w", err)
		}
		return false, nil
	}
	if status != "processing" || currentLease != leaseToken {
		return false, errors.New("favorite export lease was lost before completion")
	}
	tag, err := tx.Exec(ctx, `update favorite_modpack_export_tasks set status='ready',stage='completed',
		result_file_id=$3,result_file_size=$4,result_sha256=$5,finished_at=now(),expires_at=$6,updated_at=now(),
		error_code='',error_detail='',lease_token='',lease_expires_at=null
		where public_id=$1 and lease_token=$2 and status='processing'`, taskID, leaseToken, fileID, fileSize, sha256, expires)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, errors.New("favorite export lease was lost before completion")
	}
	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit favorite export completion: %w", err)
	}
	return true, nil
}

func (worker *FavoriteModpackExportWorker) cleanupCancelledFavoriteExportArtifact(ctx context.Context, tx pgx.Tx, taskID string, fileID, fileSize int64, sha256 string) error {
	if _, err := tx.Exec(ctx, `update favorite_modpack_export_tasks set result_file_id=$2,result_file_size=$3,
		result_sha256=$4,updated_at=now() where public_id=$1 and status='cancelled'`, taskID, fileID, fileSize, sha256); err != nil {
		return fmt.Errorf("link cancelled favorite export artifact: %w", err)
	}
	if err := worker.server.tombstoneOSSFileTx(ctx, tx, fileID, "favorite_modpack_export_cancelled"); err != nil {
		return fmt.Errorf("tombstone cancelled favorite export artifact: %w", err)
	}
	return nil
}

func (worker *FavoriteModpackExportWorker) fail(ctx context.Context, taskID, leaseToken, code string, cause error) error {
	slog.Error("favorite modpack export task failed", "task_id", taskID, "code", code, "error", cause)
	var attempts int
	if err := worker.server.db.QueryRow(ctx, `select attempt_count from favorite_modpack_export_tasks where public_id=$1 and lease_token=$2`, taskID, leaseToken).Scan(&attempts); err != nil {
		return errors.Join(cause, fmt.Errorf("load favorite export failure attempt for lease: %w", err))
	}
	if attempts < favoriteExportMaxAttempts(worker.server.cfg.FavoriteExport.MaxBuildAttempts) {
		retryDelay := time.Duration(max(1, attempts)*10) * time.Second
		tag, err := worker.server.db.Exec(ctx, `update favorite_modpack_export_tasks set status='pending',stage='retry_wait',
			error_code=$3,error_detail='temporary processing failure',lease_token='',lease_expires_at=now()+$4::interval,updated_at=now()
			where public_id=$1 and lease_token=$2`, taskID, leaseToken, code, fmt.Sprintf("%d seconds", int(retryDelay.Seconds())))
		if err != nil {
			return errors.Join(cause, fmt.Errorf("persist favorite export retry state: %w", err))
		}
		if tag.RowsAffected() != 1 {
			return errors.Join(cause, errors.New("favorite export lease was lost before retry state persisted"))
		}
		return cause
	}
	var ownerID int64
	var name string
	err := worker.server.db.QueryRow(ctx, `update favorite_modpack_export_tasks set status='failed',stage='failed',
		error_code=$3,error_detail='export processing failed',finished_at=now(),lease_token='',lease_expires_at=null,updated_at=now()
		where public_id=$1 and lease_token=$2 returning owner_user_id,pack_name`, taskID, leaseToken, code).Scan(&ownerID, &name)
	if err != nil {
		return errors.Join(cause, fmt.Errorf("persist terminal favorite export failure: %w", err))
	}
	if err = worker.server.sendTemplatedNotification(ctx, ownerID, "modpack_export_failed", map[string]string{"pack_name": name, "stage": "processing", "reason": code}, map[string]any{"url": "/user?section=favorites&exportTask=" + taskID, "taskId": taskID}); err != nil {
		return errors.Join(cause, fmt.Errorf("notify failed favorite export %s: %w", taskID, err))
	}
	return cause
}
