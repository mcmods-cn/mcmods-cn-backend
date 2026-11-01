package httpapi

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"mime"
	"path/filepath"
	"strings"
	"time"
)

func (s *Server) findCompletedOSSUpload(ctx context.Context, uploaderID int64, req ossCompleteUploadRequest) (int64, map[string]any, bool, error) {
	if uploaderID <= 0 || req.ObjectKey == "" || req.SHA256 == "" || req.Category == "" {
		return 0, nil, false, nil
	}
	objectKeys := []string{req.ObjectKey}
	contentType := req.ContentType
	if contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(req.OriginalName)))
	}
	if shouldPersistMarkdownImageAsWebP(req.OriginalName, contentType, req.Source, req.Category) {
		objectKeys = append(objectKeys, persistedWebPObjectKey(req.ObjectKey))
	}
	if isModResourceRenderUploadSource(req.Source) {
		objectKeys = append(objectKeys, persistedModResourceRenderObjectKey(req.ObjectKey))
	}
	var internalID, size, sourceSize int64
	var publicID, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, storedContentType, sha, status, scanStatus string
	var createdAt, updatedAt time.Time
	var storedSHA string
	query := `select id,public_id,bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
			content_type,size_bytes,source_size_bytes,sha256,status,scan_status,created_at,updated_at,stored_sha256
		from oss_files
		where uploader_id=$1 and category=$2 and source=$3 and sha256=$4 and status='active' and source_hash_verified
		  and (object_key=any($5::text[]) or exists(select 1 from oss_upload_byte_bindings binding
          where binding.file_id=oss_files.id and binding.status='committed'
          and binding.owner_id=oss_files.uploader_id and binding.upload_object_key=any($5::text[])
          and binding.logical_category=oss_files.category and binding.source=oss_files.source
          and binding.source_verified_sha256=oss_files.sha256 and binding.source_verified_size_bytes=oss_files.source_size_bytes
          and binding.stored_object_key=oss_files.object_key and binding.stored_verified_sha256=oss_files.stored_sha256
          and binding.stored_verified_size_bytes=oss_files.size_bytes))
		  and ($6::bigint <= 0 or coalesce(nullif(source_size_bytes,0),size_bytes)=$6)`
	if _, ok := blueprintCoverPublicID(req.Source); ok {
		query += ` and scan_status in ('clean','trusted_generated')
		  and lower(split_part(content_type,';',1)) in ('image/png','image/jpeg','image/jpg','image/gif','image/webp')`
	}
	query += ` order by created_at asc limit 1`
	err := s.db.QueryRow(ctx, query, uploaderID, req.Category, req.Source, req.SHA256, objectKeys, req.SizeBytes).
		Scan(&internalID, &publicID, &bucket, &endpoint, &region, &objectKey, &category, &source, &originalName, &sourceOriginalName,
			&storedContentType, &size, &sourceSize, &sha, &status, &scanStatus, &createdAt, &updatedAt, &storedSHA)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, fmt.Errorf("find completed OSS upload: %w", err)
	}
	file := ossFileRecord(publicID, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName,
		storedContentType, size, sourceSize, sha, status, scanStatus, createdAt, updatedAt)
	file["sourceSha256"], file["storedSha256"] = sha, storedSHA
	return internalID, file, true, nil
}

type ossFileHashLookup struct {
	UploaderID    *int64
	Category      string
	Source        string
	ScanStatuses  []string
	RequireRaster bool
	// RequireTrusted prevents an unscanned upload from becoming a public
	// catalog image merely because its bytes match a later request.
	RequireTrusted                bool
	ExcludePolicyCancelledSources bool
}

func (s *Server) findExistingOSSFileByHashExact(ctx context.Context, sha256 string, sizeBytes int64, lookup ossFileHashLookup) (map[string]any, bool, error) {
	if sha256 == "" || sizeBytes <= 0 || len(lookup.ScanStatuses) == 0 {
		return nil, false, nil
	}
	var size, sourceSize int64
	var id, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, sha, status, scanStatus string
	var createdAt, updatedAt time.Time
	var storedSHA string
	query := `select public_id, bucket, endpoint, region, object_key, category, source, original_name, source_original_name, content_type, size_bytes, source_size_bytes, sha256, status, scan_status, created_at, updated_at, stored_sha256
		from oss_files
		where sha256 = $1 and coalesce(nullif(source_size_bytes, 0), size_bytes) = $2 and status = 'active' and source_hash_verified
		  and scan_status = any($3::text[])
          and exists(select 1 from oss_upload_byte_bindings binding where binding.file_id=oss_files.id
          and binding.status='committed' and binding.stored_object_key=oss_files.object_key
          and binding.owner_id=oss_files.uploader_id and binding.logical_category=oss_files.category and binding.source=oss_files.source
          and binding.source_verified_sha256=oss_files.sha256 and binding.source_verified_size_bytes=oss_files.source_size_bytes
          and binding.stored_verified_sha256=oss_files.stored_sha256 and binding.stored_verified_size_bytes=oss_files.size_bytes)`
	args := []any{sha256, sizeBytes, lookup.ScanStatuses}
	if lookup.UploaderID != nil {
		args = append(args, *lookup.UploaderID)
		query += fmt.Sprintf(` and uploader_id = $%d`, len(args))
	}
	if lookup.Category != "" {
		args = append(args, lookup.Category)
		query += fmt.Sprintf(` and category = $%d`, len(args))
	}
	if lookup.Source != "" {
		args = append(args, lookup.Source)
		query += fmt.Sprintf(` and source = $%d`, len(args))
	}
	if lookup.ExcludePolicyCancelledSources {
		query += ` and not exists(select 1 from catalog_import_packages package
			join catalog_import_jobs job on job.package_id=package.id
			where package.archive_file_id=oss_files.id
			  and job.status='cancelled' and right(job.error_code,18)='_reupload_required')`
	}
	if lookup.RequireTrusted {
		query += ` and scan_status in ('clean','trusted_generated')`
	}
	if lookup.RequireRaster {
		query += ` and lower(split_part(content_type,';',1)) in ('image/png','image/jpeg','image/jpg','image/gif','image/webp')`
	}
	query += ` order by created_at asc limit 1`
	err := s.db.QueryRow(ctx, query, args...).Scan(&id, &bucket, &endpoint, &region, &objectKey, &category, &source, &originalName, &sourceOriginalName, &contentType, &size, &sourceSize, &sha, &status, &scanStatus, &createdAt, &updatedAt, &storedSHA)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("find existing OSS file by hash: %w", err)
	}
	file := ossFileRecord(id, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, size, sourceSize, sha, status, scanStatus, createdAt, updatedAt)
	file["sourceSha256"], file["storedSha256"] = sha, storedSHA
	return file, true, nil
}
