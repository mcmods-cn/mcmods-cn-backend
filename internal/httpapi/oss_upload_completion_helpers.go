package httpapi

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
)

func (s *Server) restoreReportEvidenceForCompletedOSSFile(
	ctx context.Context,
	fileID int64,
	uploaderID int64,
	req ossCompleteUploadRequest,
) (string, string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)
	var originalName, contentType, sha256, scanStatus, actualObjectKey string
	var byteSize int64
	err = tx.QueryRow(ctx, `select coalesce(nullif(source_original_name,''),original_name),content_type,
			coalesce(nullif(source_size_bytes,0),size_bytes),sha256,scan_status,object_key
		from oss_files
		where id=$1 and uploader_id=$2 and (object_key=$3 or exists(select 1 from oss_upload_byte_bindings binding where binding.file_id=oss_files.id and binding.status='committed' and binding.owner_id=$2 and binding.upload_object_key=$3 and binding.stored_object_key=oss_files.object_key)) and category=$4 and source=$5 and sha256=$6
		  and status='active' and ($7::bigint<=0 or coalesce(nullif(source_size_bytes,0),size_bytes)=$7)
		for update`, fileID, uploaderID, req.ObjectKey, req.Category, "report_evidence", req.SHA256, req.SizeBytes).
		Scan(&originalName, &contentType, &byteSize, &sha256, &scanStatus, &actualObjectKey)
	if err != nil {
		return "", "", fmt.Errorf("lock completed report evidence OSS file: %w", err)
	}
	var evidenceID, evidenceOriginalName, evidenceContentType, evidenceSHA, evidenceScanStatus, evidenceStatus string
	var evidenceUploaderID, evidenceByteSize int64
	err = tx.QueryRow(ctx, `select public_id,uploader_id,original_name,content_type,byte_size,sha256,scan_status,status
		from report_evidence where object_key=$1 for update`, actualObjectKey).
		Scan(&evidenceID, &evidenceUploaderID, &evidenceOriginalName, &evidenceContentType, &evidenceByteSize,
			&evidenceSHA, &evidenceScanStatus, &evidenceStatus)
	if err == nil {
		if evidenceUploaderID != uploaderID || evidenceOriginalName != originalName || evidenceContentType != contentType ||
			evidenceByteSize != byteSize || evidenceSHA != sha256 || evidenceScanStatus != scanStatus || evidenceStatus != "temporary" {
			return "", "", errors.New("completed report evidence identity conflicts with its OSS file")
		}
		if err = tx.Commit(ctx); err != nil {
			return "", "", err
		}
		return evidenceID, evidenceScanStatus, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", "", fmt.Errorf("read completed report evidence: %w", err)
	}
	if err = tx.QueryRow(ctx, `insert into report_evidence(uploader_id,object_key,original_name,content_type,byte_size,sha256,scan_status)
		values($1,$2,$3,$4,$5,$6,$7) returning public_id`, uploaderID, actualObjectKey, originalName, contentType,
		byteSize, sha256, scanStatus).Scan(&evidenceID); err != nil {
		return "", "", fmt.Errorf("restore completed report evidence: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", err
	}
	return evidenceID, scanStatus, nil
}

func (s *Server) completedOSSUploadResponse(ctx context.Context, cfg ossConfigPayload, file map[string]any) (map[string]any, error) {
	objectKey := fmt.Sprint(file["objectKey"])
	access, err := s.resolveOSSObjectAccessWithConfig(ctx, cfg, objectKey, ossObjectAccessOptions{})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":                 file["id"],
		"bucket":             file["bucket"],
		"objectKey":          file["objectKey"],
		"category":           file["category"],
		"source":             file["source"],
		"originalName":       file["originalName"],
		"sourceOriginalName": file["sourceOriginalName"],
		"contentType":        file["contentType"],
		"sizeBytes":          file["sizeBytes"],
		"sourceSizeBytes":    file["sourceSizeBytes"],
		"converted":          file["converted"],
		"sha256":             file["sha256"],
		"sourceSha256":       file["sourceSha256"], "storedSha256": file["storedSha256"],
		"scanStatus": file["scanStatus"],
		"url":        access.URL,
		"accessUrl":  access.URL,
		"storageUrl": ossStoredObjectURL(cfg, objectKey),
		"idempotent": true,
	}, nil
}

func (s *Server) attachCompletedOSSUploadAssociations(ctx context.Context, response map[string]any, fileID, ownerID int64, source string) error {
	// The original registration committed the association. Replaying a file
	// receipt may report the current blueprint, but must never recreate it or
	// replace a cover the owner selected later.
	if coverPublicID, ok := blueprintCoverPublicID(source); ok {
		var current bool
		err := s.db.QueryRow(ctx, `select exists(select 1 from blueprints blueprint
			join oss_files file on file.id=blueprint.cover_file_id and file.object_key=blueprint.cover_object_key
			where blueprint.public_id=$1 and blueprint.owner_id=$3 and blueprint.status<>'deleted'
			and file.id=$2 and file.uploader_id=$3 and file.status='active' and file.scan_status in ('clean','trusted_generated')
			and lower(split_part(file.content_type,';',1)) in ('image/png','image/jpeg','image/jpg','image/gif','image/webp'))`,
			coverPublicID, fileID, ownerID).Scan(&current)
		if err != nil {
			return err
		}
		if current {
			response["blueprintId"] = coverPublicID
		}
		return nil
	}
	var publicID, status string
	err := s.db.QueryRow(ctx, `select public_id,status from blueprints
		where owner_id=$1 and original_file_id=$2 and status<>'deleted' order by created_at,id limit 1`,
		ownerID, fileID).Scan(&publicID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	response["blueprintId"] = publicID
	response["blueprint"] = map[string]any{"id": publicID, "status": status}
	return nil
}

func (s *Server) completeBlueprintUploadTx(ctx context.Context, tx pgx.Tx, fileID, ownerID int64, objectKey, originalName, contentType string, size int64, sha string, originalUploadKeys ...string) (map[string]any, error) {
	lookupKey := objectKey
	if len(originalUploadKeys) > 0 {
		lookupKey = originalUploadKeys[0]
	}
	var blueprintID int64
	var publicID, status, format string
	err := tx.QueryRow(ctx, `select id,public_id,status,source_format from blueprints where owner_id=$1 and original_object_key=$2 and status in ('uploading','queued','failed') order by created_at desc limit 1`, ownerID, lookupKey).
		Scan(&blueprintID, &publicID, &status, &format)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if size <= 0 || size > maxBlueprintSourceBytes {
		return nil, errBlueprintSourceTooLarge
	}
	if status == "queued" {
		return map[string]any{"id": publicID, "status": status}, nil
	}
	lockKey := fmt.Sprintf("blueprint:%d:%d", ownerID, fileID)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return nil, err
	}
	// Upload completion may have waited behind another transaction. Re-read and
	// lock its lifecycle state before binding the file or enqueueing work.
	if err = tx.QueryRow(ctx, `select status from blueprints where id=$1 and owner_id=$2 for update`, blueprintID, ownerID).Scan(&status); err != nil {
		return nil, err
	}
	if status == "queued" {
		return map[string]any{"id": publicID, "status": status}, nil
	}
	if status != "uploading" && status != "failed" {
		return nil, errBlueprintNotFound
	}
	var existingPublicID, existingStatus string
	existingErr := tx.QueryRow(ctx, `select public_id,status from blueprints
		where owner_id=$1 and original_file_id=$2 and status<>'deleted' and id<>$3
		order by created_at limit 1 for update`, ownerID, fileID, blueprintID).Scan(&existingPublicID, &existingStatus)
	if existingErr == nil {
		if _, err = tx.Exec(ctx, `delete from blueprints where id=$1`, blueprintID); err != nil {
			return nil, err
		}
		return map[string]any{"id": existingPublicID, "status": existingStatus}, nil
	}
	if !errors.Is(existingErr, pgx.ErrNoRows) {
		return nil, existingErr
	}
	if _, err = tx.Exec(ctx, `update blueprints set original_file_id=$2,status='queued',upload_expires_at=null,last_error='',updated_at=now() where id=$1`, blueprintID, fileID); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `insert into blueprint_variants(blueprint_id,format,file_id,object_key,original,recommended,status,original_name,content_type,size_bytes,sha256,created_by)
		values($1,$2,$3,$4,true,true,'ready',$5,$6,$7,$8,$9) on conflict(blueprint_id,object_key) do update set file_id=excluded.file_id,status='ready'`,
		blueprintID, format, fileID, objectKey, originalName, contentType, size, sha, ownerID); err != nil {
		return nil, err
	}
	jobPublicID, err := enqueueBlueprintJobTx(ctx, tx, blueprintID, ownerID, "normalize", "")
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": publicID, "status": "queued", "jobId": jobPublicID}, nil
}

func (s *Server) bindOSSBlueprintCoverTx(ctx context.Context, tx pgx.Tx, coverPublicID string, fileID, ownerID int64) error {
	result, err := tx.Exec(ctx, `update blueprints blueprint
 set cover_file_id=$2,cover_object_key=file.object_key,updated_at=now()
 from oss_files file
 where blueprint.public_id=$1 and blueprint.owner_id=$3 and blueprint.status<>'deleted' and file.id=$2
 and file.uploader_id=$3 and file.status='active'
 and file.scan_status in ('clean','trusted_generated')
 and lower(split_part(file.content_type,';',1)) in ('image/png','image/jpeg','image/jpg','image/gif','image/webp')`, coverPublicID, fileID, ownerID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("blueprint cover is not a trusted raster")
	}
	return nil
}

// Match blueprint deletion's association-before-quota order. A fresh file ID
// is not visible to a competing legacy completion until this transaction commits.
func (s *Server) lockOSSCompletionAssociationsTx(ctx context.Context, tx pgx.Tx, ownerID int64, objectKey, source string, wasRegistered bool) error {
	if !wasRegistered {
		if _, err := tx.Exec(ctx, `select id from blueprints where owner_id=$1 and original_object_key=$2 and status in ('uploading','queued','failed') order by id for update`, ownerID, objectKey); err != nil {
			return err
		}
	}
	if cover, ok := blueprintCoverPublicID(source); ok {
		if _, err := tx.Exec(ctx, `select id from blueprints where owner_id=$1 and public_id=$2 for update`, ownerID, cover); err != nil {
			return err
		}
	}
	return nil
}

// Signing stays outside this transaction; the ticket is disclosed only after its
// original activity and any authoritative business hook have committed together.
func (s *Server) commitOSSDownloadTicket(r *http.Request, objectKey string) error {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if err = runOSSDownloadTransactionHook(r.Context(), tx); err != nil {
		return err
	}
	if err = enqueueRequestActivityTx(r, tx); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	// ARCH020: general OSS operational telemetry is best-effort. Its failure
	// cannot roll back a successfully committed business settlement/capability.
	s.recordOSSDownloadStat(r.Context(), objectKey)
	return nil
}
