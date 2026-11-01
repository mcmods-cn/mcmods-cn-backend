package httpapi

import (
	"context"
	"errors"
	"time"
)

var errOSSRawUploadRetired = errors.New("original OSS upload has been retired")

func (s *Server) persistOSSByteBindingPreparation(ctx context.Context, b *ossUploadByteBinding, req ossCompleteUploadRequest, owner, sourceSize int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('oss-completion:'||$1,0))`, b.cfg.Bucket+":"+b.cfg.Endpoint+":"+b.UploadKey); err != nil {
		return err
	}
	if err := requireOSSOriginalUploadLiveTx(ctx, tx, b.cfg, b.UploadKey); err != nil {
		return err
	}
	var journal bool
	if err = tx.QueryRow(ctx, `select to_regclass('public.oss_upload_maintenance_actions') is not null`).Scan(&journal); err != nil {
		return err
	}
	if journal {
		var retired bool
		if err = tx.QueryRow(ctx, `select exists(select 1 from public.oss_upload_maintenance_actions where operation='cleanup_raw' and bucket=$1 and endpoint=$2 and target_key=$3)`, b.cfg.Bucket, b.cfg.Endpoint, b.UploadKey).Scan(&retired); err != nil {
			return err
		}
		if retired {
			return errOSSRawUploadRetired
		}
	}
	if _, err = tx.Exec(ctx, `insert into oss_upload_byte_bindings(id,owner_id,bucket,endpoint,region,use_cname,upload_object_key,logical_category,source,original_name,declared_sha256,declared_size_bytes,sealed_source_key,stored_object_key,run_token,lease_expires_at,file_endpoint)
 values($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13,$14,clock_timestamp()+make_interval(secs=>$15),$16)`, b.ID, owner, b.cfg.Bucket, b.cfg.Endpoint, b.cfg.Region, b.cfg.UseCName || isCustomOSSEndpoint(b.cfg.Endpoint), b.UploadKey, req.Category, req.Source, req.OriginalName, req.SHA256, sourceSize, b.SourceKey, b.Token, int(ossByteBindingLease/time.Second), b.cfg.displayEndpoint()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
