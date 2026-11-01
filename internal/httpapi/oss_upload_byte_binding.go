package httpapi

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

const ossSealedUploadComponent = "_mcmods_sealed_v1"
const ossByteBindingLease = 2 * time.Minute

var errOSSByteBindingLeaseLost = errors.New("OSS byte-binding attempt lost its lease")
var errOSSByteBindingPersistence = errors.New("persist OSS byte-binding state")
var errOSSSealedReplayIdentity = errors.New("sealed object is not this user completed upload")

type ossUploadByteBinding struct {
	ID, Token, UploadKey, SourceKey, StoredKey string
	cfg                                        ossConfigPayload
	ctx                                        context.Context
	stop                                       func()
}

func isOSSSealedObjectKey(key string) bool {
	for _, component := range strings.Split(strings.ReplaceAll(key, "\\", "/"), "/") {
		if strings.EqualFold(component, ossSealedUploadComponent) {
			return true
		}
	}
	return false
}

// Copy preparation owns no database connection during any provider operation.
// The receipt precedes the first write, so failure/recovery can identify exactly
// this attempt's server-only objects without touching a client's input upload.
func (s *Server) sealOSSUploadedSource(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, req ossCompleteUploadRequest, owner int64, head *aliyunoss.HeadObjectResult) (*ossUploadByteBinding, error) {
	if isOSSSealedObjectKey(req.ObjectKey) || owner <= 0 || head.ContentLength <= 0 || head.ContentLength > maxOSSUploadBytes {
		return nil, errors.New("invalid OSS byte-binding source")
	}
	b := &ossUploadByteBinding{ID: newExportID(), Token: newExportID(), UploadKey: req.ObjectKey, cfg: cfg}
	b.SourceKey = path.Join(path.Dir(req.ObjectKey), ossSealedUploadComponent, b.ID, path.Base(req.ObjectKey))
	b.StoredKey = b.SourceKey
	if _, err := s.db.Exec(ctx, `insert into oss_upload_byte_bindings(id,owner_id,bucket,endpoint,region,use_cname,upload_object_key,logical_category,source,original_name,declared_sha256,declared_size_bytes,sealed_source_key,stored_object_key,run_token,lease_expires_at,file_endpoint)
 values($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13,$14,clock_timestamp()+make_interval(secs=>$15),$16)`, b.ID, owner, cfg.Bucket, cfg.Endpoint, cfg.Region, cfg.UseCName || isCustomOSSEndpoint(cfg.Endpoint), req.ObjectKey, req.Category, req.Source, req.OriginalName, req.SHA256, head.ContentLength, b.SourceKey, b.Token, int(ossByteBindingLease/time.Second), cfg.displayEndpoint()); err != nil {
		return nil, fmt.Errorf("%w: %w", errOSSByteBindingPersistence, err)
	}
	b.ctx, b.stop = s.startOSSByteBindingHeartbeat(ctx, b)
	err := copyOSSUploadToSealedKey(b.ctx, client, cfg, req.ObjectKey, b.SourceKey, head)
	if err == nil {
		err = verifyOSSUploadedSourceBytes(b.ctx, client, cfg, b.SourceKey, head.ContentLength, req.SHA256)
	}
	if err != nil {
		s.discardOSSByteBinding(b)
		return nil, fmt.Errorf("seal and verify source bytes: %w", err)
	}
	return b, nil
}

func (s *Server) startOSSByteBindingHeartbeat(parent context.Context, b *ossUploadByteBinding) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(ossByteBindingLease / 4)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.refreshOSSByteBindingLease(ctx, b); err != nil {
					cancel(err)
					return
				}
			}
		}
	}()
	var once sync.Once
	return ctx, func() { once.Do(func() { cancel(context.Canceled); <-done }) }
}

func ownedOSSByteBindingCandidate(b *ossUploadByteBinding, key string) bool {
	dir := path.Dir(key)
	return isOSSSealedObjectKey(key) && path.Base(dir) == b.ID && path.Base(path.Dir(dir)) == ossSealedUploadComponent && dir == path.Dir(b.SourceKey)
}

func (s *Server) planOSSByteBindingStoredKey(b *ossUploadByteBinding, key string) error {
	if !ownedOSSByteBindingCandidate(b, key) {
		return errors.New("converted object is outside its sealed attempt")
	}
	tx, err := s.db.Begin(b.ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(b.ctx)
	if err = lockOSSByteBindingTx(b.ctx, tx, b); err != nil {
		return err
	}
	tag, err := tx.Exec(b.ctx, `update oss_upload_byte_bindings set stored_object_key=$3,updated_at=clock_timestamp() where id=$1::uuid and run_token=$2 and status='preparing' and lease_expires_at>clock_timestamp()`, b.ID, b.Token, key)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errOSSByteBindingLeaseLost
	}
	if err = tx.Commit(b.ctx); err != nil {
		return err
	}
	b.StoredKey = key
	return nil
}

// Lock first, then evaluate the current database clock in a new statement.
// A lease value computed before a row/advisory-lock wait cannot admit late work.
func lockOSSByteBindingTx(ctx context.Context, tx pgx.Tx, b *ossUploadByteBinding) error {
	var token, status string
	if err := tx.QueryRow(ctx, `select run_token,status from oss_upload_byte_bindings where id=$1::uuid for update`, b.ID).Scan(&token, &status); err != nil {
		return err
	}
	if token != b.Token || status != "preparing" {
		return errOSSByteBindingLeaseLost
	}
	var live bool
	if err := tx.QueryRow(ctx, `select lease_expires_at>clock_timestamp() from oss_upload_byte_bindings where id=$1::uuid`, b.ID).Scan(&live); err != nil {
		return err
	}
	if !live {
		return errOSSByteBindingLeaseLost
	}
	return nil
}

func commitOSSByteBindingTx(ctx context.Context, tx pgx.Tx, b *ossUploadByteBinding, fileID int64, sourceSHA string, sourceSize int64, storedSHA string, storedSize int64) error {
	tag, err := tx.Exec(ctx, `update oss_upload_byte_bindings binding set status='committed',file_id=$3,source_verified_sha256=$4,source_verified_size_bytes=$5,stored_verified_sha256=$6,stored_verified_size_bytes=$7,updated_at=clock_timestamp()
 where binding.id=$1::uuid and binding.run_token=$2 and binding.status='preparing' and binding.lease_expires_at>clock_timestamp()
 and binding.file_endpoint<>'' and binding.file_endpoint=$8
 and exists(select 1 from oss_files file where file.id=$3 and file.bucket=binding.bucket and file.endpoint=binding.file_endpoint
 and file.object_key=binding.stored_object_key and file.uploader_id=binding.owner_id
 and file.category=binding.logical_category and file.source=binding.source and file.status='active'
 and file.sha256=$4 and file.source_size_bytes=$5 and file.stored_sha256=$6 and file.size_bytes=$7)`, b.ID, b.Token, fileID, sourceSHA, sourceSize, storedSHA, storedSize, b.cfg.displayEndpoint())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errOSSByteBindingLeaseLost
	}
	return nil
}

// A reserved key is only a completed-upload replay, never a new client input.
// Reconstruct its original logical upload scope before the normal ACL checks.
func (s *Server) canonicalOSSCompletedUploadAlias(ctx context.Context, cfg ossConfigPayload, owner int64, req *ossCompleteUploadRequest) error {
	if !isOSSSealedObjectKey(req.ObjectKey) {
		return nil
	}
	var originalKey, originalName string
	err := s.db.QueryRow(ctx, `select binding.upload_object_key,binding.original_name
 from oss_upload_byte_bindings binding join oss_files file on file.id=binding.file_id
 where binding.bucket=$1 and binding.endpoint=$2 and binding.owner_id=$3
 and binding.stored_object_key=$4 and binding.declared_sha256=$5
 and ($6::bigint<=0 or binding.declared_size_bytes=$6)
 and file.status='active' and file.uploader_id=$3
 and `+ossByteBindingCurrentFileProofSQL+` `, cfg.Bucket, cfg.Endpoint, owner, req.ObjectKey, req.SHA256, req.SizeBytes).Scan(&originalKey, &originalName)
	if errors.Is(err, pgx.ErrNoRows) {
		return errOSSSealedReplayIdentity
	}
	if err != nil {
		return fmt.Errorf("%w: %w", errOSSByteBindingPersistence, err)
	}
	req.ObjectKey, req.OriginalName = originalKey, originalName
	return nil
}

// Stable original upload references remain usable after adoption. Resolution
// only identifies an existing active file; callers still perform their normal
// owner/source/project authorization on the resulting file.
func (s *Server) resolvedOSSUploadObjectKey(ctx context.Context, cfg ossConfigPayload, key string, owner int64) (string, error) {
	var stored string
	err := s.db.QueryRow(ctx, `select binding.stored_object_key from oss_upload_byte_bindings binding
 join oss_files file on file.id=binding.file_id
 where binding.bucket=$1 and binding.endpoint=$2 and binding.upload_object_key=$3
 and file.status='active' and `+ossByteBindingCurrentFileProofSQL+`
 and ($4::bigint<=0 or (binding.owner_id=$4 and file.uploader_id=$4))`, cfg.Bucket, cfg.Endpoint, key, owner).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return key, nil
	}
	if err != nil {
		return "", err
	}
	return stored, nil
}

func (s *Server) refreshOSSByteBindingLease(ctx context.Context, b *ossUploadByteBinding) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockOSSByteBindingTx(ctx, tx, b); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `update oss_upload_byte_bindings set lease_expires_at=clock_timestamp()+make_interval(secs=>$3),updated_at=clock_timestamp() where id=$1::uuid and run_token=$2 and status='preparing' and lease_expires_at>clock_timestamp()`, b.ID, b.Token, int(ossByteBindingLease/time.Second))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errOSSByteBindingLeaseLost
	}
	return tx.Commit(ctx)
}
