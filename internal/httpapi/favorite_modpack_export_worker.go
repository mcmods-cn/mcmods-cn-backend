package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

type FavoriteModpackExportWorker struct {
	server *Server
	queue  *queue.Client
}

const favoriteExportMaxBuildAttempts = 3

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
	worker.processPending(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.processPending(ctx)
		}
	}
}

func (worker *FavoriteModpackExportWorker) processPending(ctx context.Context) {
	maxAttempts := favoriteExportMaxAttempts(worker.server.cfg.FavoriteExport.MaxBuildAttempts)
	worker.expireCompleted(ctx)
	worker.failExhaustedLeases(ctx)
	rows, err := worker.server.db.Query(ctx, `select public_id from favorite_modpack_export_tasks
		where (status='pending' and coalesce(lease_expires_at,now())<=now())
		   or (status='processing' and lease_expires_at<now() and attempt_count<$1)
		order by created_at limit 10`, maxAttempts)
	if err != nil {
		return
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		_ = worker.process(ctx, id)
	}
}

func (worker *FavoriteModpackExportWorker) failExhaustedLeases(ctx context.Context) {
	maxAttempts := favoriteExportMaxAttempts(worker.server.cfg.FavoriteExport.MaxBuildAttempts)
	rows, err := worker.server.db.Query(ctx, `update favorite_modpack_export_tasks set status='failed',stage='failed',
		error_code='WORKER_LEASE_EXHAUSTED',error_detail='export processing failed',lease_token='',lease_expires_at=null,
		finished_at=now(),updated_at=now()
		where id in (select id from favorite_modpack_export_tasks where status='processing' and lease_expires_at<now()
			and attempt_count>=$1 order by lease_expires_at,id for update skip locked limit 20)
		returning public_id,owner_user_id,pack_name`, maxAttempts)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var taskID, name string
		var ownerID int64
		if rows.Scan(&taskID, &ownerID, &name) == nil {
			worker.server.sendTemplatedNotification(ctx, ownerID, "modpack_export_failed", map[string]string{"pack_name": name, "stage": "processing", "reason": "WORKER_LEASE_EXHAUSTED"}, map[string]any{"url": "/account/favorites/exports/" + taskID, "taskId": taskID})
		}
	}
}

// expireCompleted removes only the generated temporary artifact. The task and
// its structured report stay in PostgreSQL so users can inspect or repeat an
// old export after its download has expired.
func (worker *FavoriteModpackExportWorker) expireCompleted(ctx context.Context) {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `select task.id,file.id,task.owner_user_id,task.pack_name,task.public_id from favorite_modpack_export_tasks task
		join oss_files file on file.id=task.result_file_id
		where task.status='ready' and task.expires_at<=now()
		order by task.expires_at,task.id for update of task skip locked limit 50`)
	if err != nil {
		return
	}
	type expiredArtifact struct {
		taskID, fileID, ownerID int64
		name, publicID          string
	}
	artifacts := make([]expiredArtifact, 0)
	for rows.Next() {
		var artifact expiredArtifact
		if rows.Scan(&artifact.taskID, &artifact.fileID, &artifact.ownerID, &artifact.name, &artifact.publicID) == nil {
			artifacts = append(artifacts, artifact)
		}
	}
	rows.Close()
	for _, artifact := range artifacts {
		if err = worker.server.tombstoneOSSFileTx(ctx, tx, artifact.fileID, "favorite_modpack_export_expired"); err != nil {
			return
		}
		if _, err = tx.Exec(ctx, `update favorite_modpack_export_tasks set status='expired',stage='expired',updated_at=now() where id=$1`, artifact.taskID); err != nil {
			return
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return
	}
	for _, artifact := range artifacts {
		worker.server.sendTemplatedNotification(ctx, artifact.ownerID, "modpack_export_expired", map[string]string{"pack_name": artifact.name}, map[string]any{"taskId": artifact.publicID, "url": "/user?section=favorites&exportTask=" + artifact.publicID})
	}
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
	err = worker.server.db.QueryRow(ctx, `select owner_user_id,pack_name,pack_version_id,minecraft_version,loader_type,loader_version,attempt_count
		from favorite_modpack_export_tasks where public_id=$1 and lease_token=$2`, taskID, leaseToken).
		Scan(&ownerID, &name, &version, &minecraftVersion, &loader, &loaderVersion, &attempt)
	if err != nil {
		return worker.fail(ctx, taskID, leaseToken, "TASK_LOAD_FAILED", err)
	}
	if attempt > maxAttempts {
		return worker.fail(ctx, taskID, leaseToken, "RETRY_LIMIT_REACHED", errors.New("export worker retry limit reached"))
	}
	rows, err := worker.server.db.Query(ctx, `select selected_file_name,sha1,sha512,env_client,env_server,download_url,file_size from favorite_modpack_export_items where task_id=(select id from favorite_modpack_export_tasks where public_id=$1) and result_type in ('exported','auto_dependency') order by id`, taskID)
	if err != nil {
		return worker.fail(ctx, taskID, leaseToken, "REPORT_LOAD_FAILED", err)
	}
	files := make([]mrpackFile, 0)
	for rows.Next() {
		var filename, sha1, sha512, clientEnv, serverEnv, download string
		var size int64
		if rows.Scan(&filename, &sha1, &sha512, &clientEnv, &serverEnv, &download, &size) != nil {
			continue
		}
		files = append(files, mrpackFile{Path: "mods/" + filename, Hashes: map[string]string{"sha1": sha1, "sha512": sha512}, Env: mrpackEnvironment{Client: clientEnv, Server: serverEnv}, Downloads: []string{download}, FileSize: size})
	}
	rows.Close()
	result, err := buildMRPack(mrpackBuildInput{Name: name, VersionID: version, Summary: "Exported from an MCMods favorite collection", MinecraftVersion: minecraftVersion, Loader: loader, LoaderVersion: loaderVersion, Files: files})
	if err != nil {
		return worker.fail(ctx, taskID, leaseToken, "MRPACK_BUILD_FAILED", err)
	}
	cfg := worker.server.ossConfigFromSettings(ctx)
	objectKey := path.Join(ossObjectPrefix(cfg.Prefix, "temporary/modpack-exports/"+taskID), randomObjectName()+".mrpack")
	filename := safeMRPackDownloadName(name, minecraftVersion, loader)
	fileID, err := worker.server.writeGeneratedOSSObject(ctx, objectKey, filename, "application/x-modrinth-modpack+zip", result.Data, ownerID, "favorite_modpack_export")
	if err != nil {
		return worker.fail(ctx, taskID, leaseToken, "OSS_UPLOAD_FAILED", err)
	}
	expires := time.Now().Add(favoriteExportArtifactTTL(worker.server.cfg.FavoriteExport.ArtifactTTL))
	tag, err = worker.server.db.Exec(ctx, `update favorite_modpack_export_tasks set status='ready',stage='completed',
		result_file_id=$3,result_file_size=$4,result_sha256=$5,finished_at=now(),expires_at=$6,updated_at=now(),
		error_code='',error_detail='',lease_token='',lease_expires_at=null
		where public_id=$1 and lease_token=$2`, taskID, leaseToken, fileID, result.Size, result.SHA256, expires)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("favorite export lease was lost before completion")
	}
	var exported, deps, skipped int
	_ = worker.server.db.QueryRow(ctx, `select exported_mod_count,auto_dependency_count,skipped_item_count+failed_item_count from favorite_modpack_export_tasks where public_id=$1`, taskID).Scan(&exported, &deps, &skipped)
	template := "modpack_export_completed"
	if skipped > 0 {
		template = "modpack_export_completed_with_skips"
	}
	worker.server.sendTemplatedNotification(ctx, ownerID, template, map[string]string{"pack_name": name, "minecraft_version": minecraftVersion, "loader": strings.ToUpper(loader) + " " + loaderVersion, "exported": fmt.Sprint(exported), "dependencies": fmt.Sprint(deps), "skipped": fmt.Sprint(skipped)}, map[string]any{"url": "/user?section=favorites&exportTask=" + taskID, "taskId": taskID})
	return nil
}

func (worker *FavoriteModpackExportWorker) fail(ctx context.Context, taskID, leaseToken, code string, cause error) error {
	log.Printf("favorite modpack export task %s failed at %s: %v", taskID, code, cause)
	var attempts int
	_ = worker.server.db.QueryRow(ctx, `select attempt_count from favorite_modpack_export_tasks where public_id=$1 and lease_token=$2`, taskID, leaseToken).Scan(&attempts)
	if attempts < favoriteExportMaxAttempts(worker.server.cfg.FavoriteExport.MaxBuildAttempts) {
		retryDelay := time.Duration(max(1, attempts)*10) * time.Second
		_, _ = worker.server.db.Exec(ctx, `update favorite_modpack_export_tasks set status='pending',stage='retry_wait',
			error_code=$3,error_detail='temporary processing failure',lease_token='',lease_expires_at=now()+$4::interval,updated_at=now()
			where public_id=$1 and lease_token=$2`, taskID, leaseToken, code, fmt.Sprintf("%d seconds", int(retryDelay.Seconds())))
		return cause
	}
	_, _ = worker.server.db.Exec(ctx, `update favorite_modpack_export_tasks set status='failed',stage='failed',
		error_code=$3,error_detail='export processing failed',finished_at=now(),lease_token='',lease_expires_at=null,updated_at=now()
		where public_id=$1 and lease_token=$2`, taskID, leaseToken, code)
	var ownerID int64
	var name string
	if worker.server.db.QueryRow(ctx, `select owner_user_id,pack_name from favorite_modpack_export_tasks where public_id=$1`, taskID).Scan(&ownerID, &name) == nil {
		worker.server.sendTemplatedNotification(ctx, ownerID, "modpack_export_failed", map[string]string{"pack_name": name, "stage": "processing", "reason": code}, map[string]any{"url": "/user?section=favorites&exportTask=" + taskID, "taskId": taskID})
	}
	return cause
}
