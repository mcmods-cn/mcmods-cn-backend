package httpapi

import (
	"context"
	"errors"
	"fmt"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

// A revoked legacy trust marker never becomes true just because metadata or a
// previous marker says so. A previously sealed receipt can be re-established
// only after reading its actual source and stored bytes outside the transaction.
func (s *Server) reverifyOSSCompletedByteBinding(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, owner int64, req ossCompleteUploadRequest) (bool, error) {
	var bindingID, sourceKey, storedKey, storedSHA string
	var fileID, sourceSize, storedSize int64
	err := s.db.QueryRow(ctx, `select binding.id::text,binding.file_id,binding.sealed_source_key,binding.stored_object_key,binding.stored_verified_sha256,binding.source_verified_size_bytes,binding.stored_verified_size_bytes
 from oss_upload_byte_bindings binding join oss_files file on file.id=binding.file_id
 where binding.bucket=$1 and binding.endpoint=$2 and binding.upload_object_key=$3
 and binding.status='committed' and binding.owner_id=$4 and binding.logical_category=$5 and binding.source=$6
 and binding.source_verified_sha256=$7 and ($8::bigint<=0 or binding.source_verified_size_bytes=$8)
 and file.status='active' and not file.source_hash_verified and file.uploader_id=$4
 and file.object_key=binding.stored_object_key and file.sha256=binding.source_verified_sha256
 and file.stored_sha256=binding.stored_verified_sha256 and file.source_size_bytes=binding.source_verified_size_bytes
 and file.size_bytes=binding.stored_verified_size_bytes`, cfg.Bucket, cfg.Endpoint, req.ObjectKey, owner, req.Category, req.Source, req.SHA256, req.SizeBytes).Scan(&bindingID, &fileID, &sourceKey, &storedKey, &storedSHA, &sourceSize, &storedSize)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: %w", errOSSByteBindingPersistence, err)
	}
	if err = verifyOSSUploadedSourceBytes(ctx, client, cfg, sourceKey, sourceSize, req.SHA256); err != nil {
		return false, err
	}
	if sourceKey != storedKey {
		if err = verifyOSSUploadedSourceBytes(ctx, client, cfg, storedKey, storedSize, storedSHA); err != nil {
			return false, err
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errOSSByteBindingPersistence, err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('oss-completion:'||$1,0))`, cfg.Bucket+":"+cfg.Endpoint+":"+req.ObjectKey); err != nil {
		return false, fmt.Errorf("%w: %w", errOSSByteBindingPersistence, err)
	}
	var lockedID string
	if err = tx.QueryRow(ctx, `select id::text from oss_upload_byte_bindings where id=$1::uuid and status='committed' and file_id=$2 and sealed_source_key=$3 and stored_object_key=$4 and stored_verified_sha256=$5 for share`, bindingID, fileID, sourceKey, storedKey, storedSHA).Scan(&lockedID); err != nil {
		return false, fmt.Errorf("%w: %w", errOSSByteBindingPersistence, err)
	}
	tag, err := tx.Exec(ctx, `update oss_files set source_hash_verified=true where id=$1 and uploader_id=$2 and object_key=$3 and status='active' and sha256=$4 and source_size_bytes=$5 and stored_sha256=$6 and size_bytes=$7 and not source_hash_verified`, fileID, owner, storedKey, req.SHA256, sourceSize, storedSHA, storedSize)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errOSSByteBindingPersistence, err)
	}
	if tag.RowsAffected() != 1 {
		var alreadyVerified bool
		if err = tx.QueryRow(ctx, `select exists(select 1 from oss_files where id=$1 and uploader_id=$2 and object_key=$3 and status='active' and source_hash_verified and sha256=$4 and source_size_bytes=$5 and stored_sha256=$6 and size_bytes=$7)`, fileID, owner, storedKey, req.SHA256, sourceSize, storedSHA, storedSize).Scan(&alreadyVerified); err != nil {
			return false, fmt.Errorf("%w: %w", errOSSByteBindingPersistence, err)
		}
		if !alreadyVerified {
			return false, errOSSCompletedIdentityConflict
		}
		if err = tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("%w: %w", errOSSByteBindingPersistence, err)
		}
		return false, nil
	}
	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("%w: %w", errOSSByteBindingPersistence, err)
	}
	return true, nil
}
