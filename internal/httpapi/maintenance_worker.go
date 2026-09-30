package httpapi

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/querycache"
)

const (
	maintenanceBatchSize  = 1000
	maintenanceMaxBatches = 4
)

const stickerUploadRetention = time.Hour

// MaintenanceWorker keeps expiration cleanup outside user-facing request
// transactions. Each delete is bounded so a large backlog cannot hold a long
// table lock or delay API responses.
type MaintenanceWorker struct {
	db    *pgxpool.Pool
	cache *querycache.Cache
}

func NewMaintenanceWorker(db *pgxpool.Pool, cache *querycache.Cache) *MaintenanceWorker {
	return &MaintenanceWorker{db: db, cache: cache}
}

func (worker *MaintenanceWorker) Start(ctx context.Context) {
	if worker == nil || worker.db == nil {
		return
	}
	go worker.run(ctx)
}

func (worker *MaintenanceWorker) run(ctx context.Context) {
	worker.prune(ctx)
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.prune(ctx)
		}
	}
}

func (worker *MaintenanceWorker) prune(ctx context.Context) {
	worker.pruneWithTimeout(ctx, 30*time.Second)
}

func (worker *MaintenanceWorker) pruneWithTimeout(ctx context.Context, timeout time.Duration) {
	// Security state goes first. Every category gets its own deadline and a
	// fixed batch budget; a blocked or backlogged category cannot consume the
	// next category's time. All deadlines still inherit shutdown cancellation.
	for _, prune := range []func(context.Context){
		worker.expireBans,
		worker.pruneBlueprintUploads,
		worker.pruneReportEvidence,
		worker.pruneStickerUploads,
		worker.pruneSkinTextureBlobs,
		worker.pruneCatalogImportArtifacts,
	} {
		if ctx.Err() != nil {
			return
		}
		pruneCtx, cancel := context.WithTimeout(ctx, timeout)
		prune(pruneCtx)
		cancel()
	}
	for _, statement := range []string{
		`update log_shares set status='expired' where id in (
			select id from log_shares where expires_at<=now() and status in ('processing','ready')
			order by expires_at,id limit $1
		)`,
		`delete from log_share_entries where id in (
			select entry.id from log_share_entries entry join log_shares share on share.id=entry.log_share_id
			where share.status in ('expired','deleted','source_deleted') order by entry.id limit $1
		)`,
		`delete from user_drafts where id in (
			select id from user_drafts where expires_at<=now() order by expires_at limit $1
		)`,
		`delete from yggdrasil_join_sessions where server_id in (
			select server_id from yggdrasil_join_sessions where expires_at<=now() order by expires_at limit $1
		)`,
		`delete from user_presence_sessions where session_hash in (
			select session_hash from user_presence_sessions where last_active_at<now()-interval '1 day' order by last_active_at limit $1
		)`,
	} {
		if ctx.Err() != nil {
			return
		}
		pruneCtx, cancel := context.WithTimeout(ctx, timeout)
		for batch := 0; batch < maintenanceMaxBatches && pruneCtx.Err() == nil; batch++ {
			command, err := worker.db.Exec(pruneCtx, statement, maintenanceBatchSize)
			if err != nil {
				log.Printf("background expiration cleanup failed: %v", err)
				break
			}
			if command.RowsAffected() < maintenanceBatchSize {
				break
			}
		}
		cancel()
	}
}

func (worker *MaintenanceWorker) pruneCatalogImportArtifacts(ctx context.Context) {
	server := &Server{db: worker.db}
	for batch := 0; batch < maintenanceMaxBatches && ctx.Err() == nil; batch++ {
		recovered, err := server.recoverOrphanedCatalogImportArtifacts(ctx)
		if err != nil {
			log.Printf("recover orphaned catalog import artifacts: %v", err)
			return
		}
		if !recovered {
			return
		}
	}
}

// pruneSkinTextureBlobs calibrates missed request-time cleanup without scanning
// the skin history. idx_skin_texture_blobs_unreferenced keeps each batch on the
// transactionally maintained active_reference_count=0 projection.
func (worker *MaintenanceWorker) pruneSkinTextureBlobs(ctx context.Context) {
	server := &Server{db: worker.db}
	for batch := 0; batch < maintenanceMaxBatches && ctx.Err() == nil; batch++ {
		tx, err := worker.db.Begin(ctx)
		if err != nil {
			log.Printf("begin unreferenced skin texture cleanup: %v", err)
			return
		}
		rows, err := tx.Query(ctx, `select blob.hash
			from skin_texture_blobs blob
			join oss_files file on file.id=blob.oss_file_id and file.status='active'
			where blob.active_reference_count=0
			order by blob.created_at,blob.hash limit $1`, maintenanceBatchSize)
		if err != nil {
			tx.Rollback(ctx)
			log.Printf("claim unreferenced skin texture cleanup: %v", err)
			return
		}
		hashes := make([]string, 0, maintenanceBatchSize)
		for rows.Next() {
			var hash string
			if err = rows.Scan(&hash); err != nil {
				break
			}
			hashes = append(hashes, hash)
		}
		rows.Close()
		if err == nil {
			err = rows.Err()
		}
		for _, hash := range hashes {
			if err != nil {
				break
			}
			err = server.tombstoneUnreferencedMinecraftTextureBlobTx(ctx, tx, hash, "unreferenced_minecraft_texture_blob")
		}
		if err != nil {
			tx.Rollback(ctx)
			log.Printf("queue unreferenced skin texture cleanup: %v", err)
			return
		}
		if err = tx.Commit(ctx); err != nil {
			log.Printf("commit unreferenced skin texture cleanup: %v", err)
			return
		}
		if len(hashes) < maintenanceBatchSize {
			return
		}
	}
}

func (worker *MaintenanceWorker) pruneBlueprintUploads(ctx context.Context) {
	for batch := 0; batch < maintenanceMaxBatches && ctx.Err() == nil; batch++ {
		tx, err := worker.db.Begin(ctx)
		if err != nil {
			log.Printf("begin expired blueprint upload cleanup: %v", err)
			return
		}
		rows, err := tx.Query(ctx, `select id from blueprints
			where status='uploading' and upload_expires_at is not null and upload_expires_at<=now()
			order by upload_expires_at,id for update skip locked limit $1`, maintenanceBatchSize)
		if err != nil {
			tx.Rollback(ctx)
			log.Printf("claim expired blueprint upload cleanup: %v", err)
			return
		}
		blueprintIDs := make([]int64, 0, maintenanceBatchSize)
		for rows.Next() {
			var blueprintID int64
			if err = rows.Scan(&blueprintID); err != nil {
				break
			}
			blueprintIDs = append(blueprintIDs, blueprintID)
		}
		rows.Close()
		if err == nil {
			err = rows.Err()
		}
		if err == nil && len(blueprintIDs) > 0 {
			err = deletePendingBlueprintUploadRowsTx(ctx, tx, blueprintIDs)
		}
		if err != nil {
			tx.Rollback(ctx)
			log.Printf("delete expired blueprint uploads: %v", err)
			return
		}
		if err = tx.Commit(ctx); err != nil {
			log.Printf("commit expired blueprint upload cleanup: %v", err)
			return
		}
		if len(blueprintIDs) < maintenanceBatchSize {
			return
		}
	}
}

func (worker *MaintenanceWorker) pruneStickerUploads(ctx context.Context) {
	server := &Server{db: worker.db}
	for batch := 0; batch < maintenanceMaxBatches && ctx.Err() == nil; batch++ {
		tx, err := worker.db.Begin(ctx)
		if err != nil {
			log.Printf("begin temporary sticker upload cleanup: %v", err)
			return
		}
		rows, err := tx.Query(ctx, `select file.id from oss_files file
			where file.status='active'
			  and (source='sticker-upload' or source like 'sticker-upload:%')
			  and file.created_at<now()-make_interval(secs=>$2)
			  and not exists(select 1 from stickers where image_file_id=file.id)
			order by file.created_at,file.id for update of file skip locked limit $1`,
			maintenanceBatchSize, int(stickerUploadRetention/time.Second))
		if err != nil {
			tx.Rollback(ctx)
			log.Printf("claim temporary sticker upload cleanup: %v", err)
			return
		}
		fileIDs := make([]int64, 0, maintenanceBatchSize)
		for rows.Next() {
			var fileID int64
			if err = rows.Scan(&fileID); err != nil {
				break
			}
			fileIDs = append(fileIDs, fileID)
		}
		rows.Close()
		if err == nil {
			err = rows.Err()
		}
		for _, fileID := range fileIDs {
			if err != nil {
				break
			}
			err = server.tombstoneUnreferencedStickerFileTx(ctx, tx, fileID, "sticker_upload_expired")
		}
		if err != nil {
			tx.Rollback(ctx)
			log.Printf("queue temporary sticker upload cleanup: %v", err)
			return
		}
		if err = tx.Commit(ctx); err != nil {
			log.Printf("commit temporary sticker upload cleanup: %v", err)
			return
		}
		if len(fileIDs) < maintenanceBatchSize {
			return
		}
	}
}

func (worker *MaintenanceWorker) expireBans(ctx context.Context) {
	for batch := 0; batch < maintenanceMaxBatches && ctx.Err() == nil; batch++ {
		rows, err := worker.db.Query(ctx, `with candidates as (
			select id from ban_records where status='active' and ends_at is not null and ends_at<=now()
			order by ends_at,id for update skip locked limit $1
		), expired as (
			update ban_records ban set status='expired'
			from candidates where ban.id=candidates.id returning ban.id,ban.user_id
		) delete from user_role_bindings binding using expired
			where binding.user_id=expired.user_id and binding.source='governance_ban'
			  and binding.source_key=expired.id::text
			returning expired.user_id`, maintenanceBatchSize)
		if err != nil {
			log.Printf("background ban expiration cleanup failed: %v", err)
			return
		}
		userIDs := make([]int64, 0, maintenanceBatchSize)
		for rows.Next() {
			var userID int64
			if err = rows.Scan(&userID); err != nil {
				break
			}
			userIDs = append(userIDs, userID)
		}
		rows.Close()
		if err == nil {
			err = rows.Err()
		}
		if err != nil {
			log.Printf("read expired ban users: %v", err)
			return
		}
		for _, userID := range userIDs {
			key := querycache.UserPermissionVersionKey(userID)
			var invalidateErr error
			if worker.cache != nil {
				invalidateErr = worker.cache.DeleteShared(ctx, key)
			}
			var version int64
			if err = worker.db.QueryRow(ctx, `select permission_version from users where id=$1`, userID).Scan(&version); err != nil {
				log.Printf("refresh expired ban permission version user_id=%d invalidate_error=%v query_error=%v", userID, invalidateErr, err)
				continue
			}
			if worker.cache != nil && worker.cache.Enabled() && !worker.cache.SetShared(ctx, key, []byte(strconv.FormatInt(version, 10)), 10*time.Second) {
				log.Printf("publish expired ban permission version user_id=%d invalidate_error=%v", userID, invalidateErr)
			}
		}
		if len(userIDs) < maintenanceBatchSize {
			return
		}
	}
}

// pruneReportEvidence removes only the OSS object while retaining immutable
// evidence metadata on the report. The existing OSS deletion outbox supplies
// durable retries; marking the file deleted immediately also closes the access
// path before physical deletion succeeds.
func (worker *MaintenanceWorker) pruneReportEvidence(ctx context.Context) {
	for batch := 0; batch < maintenanceMaxBatches && ctx.Err() == nil; batch++ {
		tx, err := worker.db.Begin(ctx)
		if err != nil {
			log.Printf("begin report evidence cleanup: %v", err)
			return
		}
		rows, err := tx.Query(ctx, `select evidence.id,file.id,file.bucket,file.endpoint,file.region,file.object_key
			from report_evidence evidence
			left join oss_files file on file.object_key=evidence.object_key
			where evidence.status in ('temporary','pending_delete') and evidence.cleanup_after<=now()
			order by evidence.cleanup_after,evidence.id for update of evidence skip locked limit $1`, maintenanceBatchSize)
		if err != nil {
			tx.Rollback(ctx)
			log.Printf("claim report evidence cleanup: %v", err)
			return
		}
		type cleanupItem struct {
			evidenceID int64
			fileID     *int64
			bucket     *string
			endpoint   *string
			region     *string
			objectKey  *string
		}
		items := make([]cleanupItem, 0, maintenanceBatchSize)
		for rows.Next() {
			var item cleanupItem
			if err = rows.Scan(&item.evidenceID, &item.fileID, &item.bucket, &item.endpoint, &item.region, &item.objectKey); err != nil {
				break
			}
			items = append(items, item)
		}
		rows.Close()
		if err == nil {
			err = rows.Err()
		}
		if err != nil {
			tx.Rollback(ctx)
			log.Printf("read report evidence cleanup batch: %v", err)
			return
		}
		for _, item := range items {
			if item.fileID != nil && item.bucket != nil && item.endpoint != nil && item.region != nil && item.objectKey != nil {
				target := ossDeletionTarget{FileID: item.fileID, Bucket: *item.bucket, Endpoint: *item.endpoint, Region: *item.region, ObjectKey: *item.objectKey, Reason: "report_evidence_retention_expired"}
				if err = enqueueOSSObjectDeletionTx(ctx, tx, target); err != nil {
					break
				}
				if _, err = tx.Exec(ctx, `update oss_files set status='deleted',updated_at=now() where id=$1`, *item.fileID); err != nil {
					break
				}
			}
			if _, err = tx.Exec(ctx, `update report_evidence set status='deleted',deleted_at=now(),delete_attempts=delete_attempts+1,last_error='' where id=$1`, item.evidenceID); err != nil {
				break
			}
		}
		if err != nil {
			tx.Rollback(ctx)
			log.Printf("queue report evidence cleanup: %v", err)
			return
		}
		if err = tx.Commit(ctx); err != nil {
			log.Printf("commit report evidence cleanup: %v", err)
			return
		}
		if len(items) < maintenanceBatchSize {
			return
		}
	}
}
