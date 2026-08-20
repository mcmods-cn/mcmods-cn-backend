package httpapi

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/querycache"
)

const maintenanceBatchSize = 1000

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
	pruneCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	worker.pruneReportEvidence(pruneCtx)
	worker.expireBans(pruneCtx)
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
		for pruneCtx.Err() == nil {
			command, err := worker.db.Exec(pruneCtx, statement, maintenanceBatchSize)
			if err != nil {
				log.Printf("background expiration cleanup failed: %v", err)
				break
			}
			if command.RowsAffected() < maintenanceBatchSize {
				break
			}
		}
	}
}

func (worker *MaintenanceWorker) expireBans(ctx context.Context) {
	for ctx.Err() == nil {
		rows, err := worker.db.Query(ctx, `with candidates as (
			select id from ban_records where status='active' and ends_at is not null and ends_at<=now()
			order by ends_at,id for update skip locked limit $1
		), expired as (
			update ban_records ban set status='expired'
			from candidates where ban.id=candidates.id returning ban.user_id
		) delete from user_role_bindings binding using roles role
			where binding.role_id=role.id and role.code='banned' and binding.user_id in (select user_id from expired)
			returning binding.user_id`, maintenanceBatchSize)
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
			var version int64
			if err = worker.db.QueryRow(ctx, `select permission_version from users where id=$1`, userID).Scan(&version); err != nil {
				log.Printf("refresh expired ban permission version: %v", err)
				continue
			}
			if worker.cache != nil {
				worker.cache.SetShared(ctx, querycache.UserPermissionVersionKey(userID), []byte(strconv.FormatInt(version, 10)), 10*time.Second)
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
	for ctx.Err() == nil {
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
