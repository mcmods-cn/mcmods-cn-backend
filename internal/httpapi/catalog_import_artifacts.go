package httpapi

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var errCatalogImportArtifactState = errors.New("catalog import artifact state changed")

type modExportUploadAsset struct {
	ObjectKey   string
	Original    string
	Digest      string
	ContentType string
	ByteLength  int64
	Data        []byte
}

// registerCatalogImportArtifacts records both the pending OSS file and its
// exact job attempt before any provider request is started. A process crash
// can therefore never leave an uploaded import object without a durable
// deletion target.
func (s *Server) registerCatalogImportArtifacts(
	ctx context.Context,
	jobID string,
	runToken string,
	cfg ossConfigPayload,
	uploaderID int64,
	source string,
	assets []modExportUploadAsset,
) (map[string]int64, error) {
	result := make(map[string]int64, len(assets))
	if len(assets) == 0 {
		return result, nil
	}
	byObjectKey := make(map[string]modExportUploadAsset, len(assets))
	for _, asset := range assets {
		asset.ObjectKey = strings.TrimSpace(asset.ObjectKey)
		if asset.ObjectKey == "" || asset.Digest == "" || asset.ByteLength < 0 || asset.ContentType == "" {
			return nil, errors.New("catalog import artifact metadata is incomplete")
		}
		if existing, ok := byObjectKey[asset.ObjectKey]; ok {
			if existing.Digest != asset.Digest || existing.ByteLength != asset.ByteLength || existing.ContentType != asset.ContentType {
				return nil, fmt.Errorf("conflicting catalog import artifact %s", asset.ObjectKey)
			}
			continue
		}
		byObjectKey[asset.ObjectKey] = asset
	}
	objectKeys := make([]string, 0, len(byObjectKey))
	for objectKey := range byObjectKey {
		objectKeys = append(objectKeys, objectKey)
	}
	sort.Strings(objectKeys)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var ownedJobID string
	if err = tx.QueryRow(ctx, `select id from catalog_import_jobs
		where id=$1 and run_token=$2 and status in ('validating','importing') for update`, jobID, runToken).
		Scan(&ownedJobID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errModExportLeaseLost
		}
		return nil, err
	}
	for _, objectKey := range objectKeys {
		asset := byObjectKey[objectKey]
		var fileID int64
		err = tx.QueryRow(ctx, `insert into oss_files(
			bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
			content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status)
			values($1,$2,$3,$4,$5,$6,$7,$7,$8,$9,$9,$10,$11,'pending','clean')
			on conflict(object_key) do nothing returning id`,
			cfg.Bucket, cfg.Endpoint, cfg.Region, objectKey,
			ossCategoryFromObjectKey(objectKey, cfg.Prefix), source, asset.Original,
			asset.ContentType, asset.ByteLength, asset.Digest, nullableUserID(uploaderID)).Scan(&fileID)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `select file.id from catalog_import_job_artifacts artifact
				join oss_files file on file.id=artifact.oss_file_id
				where artifact.job_id=$1 and artifact.run_token=$2 and artifact.object_key=$3
				  and artifact.status='planned' and file.status='pending'
				  and file.sha256=$4 and file.size_bytes=$5 and file.content_type=$6`,
				jobID, runToken, objectKey, asset.Digest, asset.ByteLength, asset.ContentType).Scan(&fileID)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("catalog import artifact object key is already owned: %s", objectKey)
			}
		}
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `insert into catalog_import_job_artifacts(job_id,run_token,object_key,oss_file_id)
			values($1,$2,$3,$4) on conflict(job_id,run_token,object_key) do nothing`, jobID, runToken, objectKey, fileID); err != nil {
			return nil, err
		}
		result[objectKey] = fileID
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func bindCatalogImportArtifactFileIDs(pngMedia []modExportPNGMedia, localeBundles []modExportLocaleBundleMedia, fileIDs map[string]int64) error {
	for index := range pngMedia {
		pngMedia[index].FileID = fileIDs[pngMedia[index].ObjectKey]
		if pngMedia[index].FileID <= 0 {
			return fmt.Errorf("PNG artifact is not registered: %s", pngMedia[index].ObjectKey)
		}
	}
	for index := range localeBundles {
		localeBundles[index].FileID = fileIDs[localeBundles[index].ObjectKey]
		if localeBundles[index].FileID <= 0 {
			return fmt.Errorf("locale artifact is not registered: %s", localeBundles[index].ObjectKey)
		}
	}
	return nil
}

func activateCatalogImportArtifactsTx(ctx context.Context, tx pgx.Tx, jobID, runToken string) error {
	if err := lockCatalogImportAttemptTx(ctx, tx, jobID, runToken); err != nil {
		return err
	}
	var planned int64
	if err := tx.QueryRow(ctx, `select count(*) from catalog_import_job_artifacts
		where job_id=$1 and run_token=$2 and status='planned'`, jobID, runToken).Scan(&planned); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `update oss_files set status='active',updated_at=now()
		where status='pending' and id in (
			select oss_file_id from catalog_import_job_artifacts
			where job_id=$1 and run_token=$2 and status='planned'
		)`, jobID, runToken)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != planned {
		return errCatalogImportArtifactState
	}
	tag, err = tx.Exec(ctx, `update catalog_import_job_artifacts set status='active',updated_at=now()
		where job_id=$1 and run_token=$2 and status='planned'`, jobID, runToken)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != planned {
		return errCatalogImportArtifactState
	}
	return nil
}

func (s *Server) compensateCatalogImportArtifactsTx(ctx context.Context, tx pgx.Tx, jobID, runToken, reason string) error {
	// Match finalization's job -> artifact -> file lock order. Uploads hold a
	// shared artifact lock until PUT returns, so deletion cannot overtake PUT.
	var lockedJobID string
	if err := tx.QueryRow(ctx, `select id from catalog_import_jobs where id=$1 for update`, jobID).Scan(&lockedJobID); err != nil {
		return err
	}
	query := `select artifact.oss_file_id from catalog_import_job_artifacts artifact
		where artifact.job_id=$1 and artifact.status='planned'`
	arguments := []any{jobID}
	if runToken != "" {
		query += ` and artifact.run_token=$2`
		arguments = append(arguments, runToken)
	}
	query += ` order by artifact.oss_file_id for update of artifact`
	rows, err := tx.Query(ctx, query, arguments...)
	if err != nil {
		return err
	}
	fileIDs := make([]int64, 0)
	for rows.Next() {
		var fileID int64
		if err = rows.Scan(&fileID); err != nil {
			rows.Close()
			return err
		}
		fileIDs = append(fileIDs, fileID)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, fileID := range fileIDs {
		if err = s.tombstoneOSSFileTx(ctx, tx, fileID, reason); err != nil {
			return err
		}
	}
	updateQuery := `update catalog_import_job_artifacts set status='abandoned',updated_at=now()
		where job_id=$1 and status='planned'`
	if runToken != "" {
		updateQuery += ` and run_token=$2`
	}
	if _, err = tx.Exec(ctx, updateQuery, arguments...); err != nil {
		return err
	}
	return nil
}

// recoverOrphanedCatalogImportArtifacts is the durable safety net for a
// cleanup transaction that could not be persisted at the original failure
// site. It takes one attempt at a time so the maintenance worker remains
// resumable and competing instances use row locks instead of duplicating
// deletion work.
func (s *Server) recoverOrphanedCatalogImportArtifacts(ctx context.Context) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var jobID, runToken string
	err = tx.QueryRow(ctx, `select artifact.job_id,artifact.run_token
		from catalog_import_job_artifacts artifact
		join catalog_import_jobs job on job.id=artifact.job_id
		where artifact.status='planned'
		  and (job.status not in ('validating','importing') or job.run_token<>artifact.run_token)
		order by artifact.oss_file_id for update of job skip locked limit 1`).Scan(&jobID, &runToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = s.compensateCatalogImportArtifactsTx(
		ctx, tx, jobID, runToken, "catalog-import-orphaned-attempt",
	); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func lockCatalogImportAttemptTx(ctx context.Context, tx pgx.Tx, jobID, runToken string) error {
	var lockedJobID string
	err := tx.QueryRow(ctx, `select id from catalog_import_jobs
		where id=$1 and run_token=$2 and status in ('validating','importing') for update`, jobID, runToken).Scan(&lockedJobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errModExportLeaseLost
	}
	return err
}

func (s *Server) catalogImportUploadGuard(jobID, runToken string) func(context.Context, string, func(context.Context) error) error {
	return func(ctx context.Context, objectKey string, upload func(context.Context) error) error {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(context.Background())
		var fileID int64
		err = tx.QueryRow(ctx, `select artifact.oss_file_id
			from catalog_import_job_artifacts artifact
			join catalog_import_jobs job on job.id=artifact.job_id
			where artifact.job_id=$1 and artifact.run_token=$2 and artifact.object_key=$3
			  and artifact.status='planned' and job.run_token=$2 and job.status in ('validating','importing')
			for share of artifact`, jobID, runToken, objectKey).Scan(&fileID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errModExportLeaseLost
		}
		if err != nil {
			return err
		}
		if err = upload(ctx); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
}
