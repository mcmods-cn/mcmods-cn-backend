package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

var errOSSRawDeletionDeferred = errors.New("original upload cleanup has a durable hold")

type ossDeletedRawProof struct {
	BindingID  string
	FileID     int64
	Owner      int64
	SourceSHA  string
	SourceSize int64
	StoredKey  string
}

// A hold is an eligibility state, not a failed provider operation. In
// particular a still-live grant must not exhaust the ordinary retry budget.
func deferOSSRawDeletionTx(ctx context.Context, tx pgx.Tx, job ossDeletionJob, reason string, next *time.Time) error {
	if _, err := requireOSSRawDeletionLeaseTx(ctx, tx, job); err != nil {
		return err
	}
	var tagRows int64
	tag, err := tx.Exec(ctx, `update oss_object_deletion_outbox set status='pending',attempts=greatest(attempts-1,0),next_attempt_at=coalesce($3::timestamptz,clock_timestamp()+interval '1 hour'),locked_at=null,locked_by='',last_error='',failure_class='',raw_hold_reason=$4,updated_at=clock_timestamp() where id=$1 and status='processing' and locked_by=$2 and locked_at+make_interval(secs=>$5)>clock_timestamp()`, job.ID, job.LockToken, next, reason, int(ossDeletionLeaseTimeout/time.Second))
	if err == nil {
		tagRows = tag.RowsAffected()
	}
	if err != nil {
		return err
	}
	if tagRows != 1 {
		return errOSSDeletionLeaseLost
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return errOSSRawDeletionDeferred
}

// Lock the queue lease, original key and exact immutable receipt/file identity.
// This uses the existing terminal-file deletion policy: historical package and
// audit rows remain, while genuinely active sharing/attempts keep a HOLD.
func lockOSSDeletedRawProof(ctx context.Context, tx pgx.Tx, job ossDeletionJob) (ossDeletedRawProof, error) {
	var proof ossDeletedRawProof
	if job.RawUploadBindingID == nil || job.FileID == nil || job.ObjectKey == "" || isOSSSealedObjectKey(job.ObjectKey) {
		return proof, errOSSDeletionTargetIncomplete
	}
	var live bool
	if err := tx.QueryRow(ctx, `select true from oss_object_deletion_outbox where id=$1 and status='processing' and locked_by=$2 and raw_upload_binding_id=$3::uuid and oss_file_id=$4 and bucket=$5 and endpoint=$6 and object_key=$7 for update`, job.ID, job.LockToken, *job.RawUploadBindingID, *job.FileID, job.Bucket, job.Endpoint, job.ObjectKey).Scan(&live); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return proof, errOSSDeletionLeaseLost
		}
		return proof, err
	}
	cfg := ossConfigPayload{Bucket: job.Bucket, Endpoint: job.Endpoint}
	if err := lockOSSOriginalUploadKeyTx(ctx, tx, cfg, job.ObjectKey); err != nil {
		return proof, err
	}
	var status string
	var exact bool
	err := tx.QueryRow(ctx, `select binding.id::text,binding.file_id,binding.owner_id,binding.source_verified_sha256,binding.source_verified_size_bytes,binding.stored_object_key,file.status,
 binding.status='committed' and binding.bucket=$3 and binding.endpoint=$4 and binding.upload_object_key=$5
 and binding.file_endpoint<>'' and file.bucket=binding.bucket and file.endpoint=binding.file_endpoint
 and file.object_key=binding.stored_object_key and file.uploader_id=binding.owner_id
 and file.category=binding.logical_category and file.source=binding.source
 and file.sha256=binding.source_verified_sha256 and file.source_size_bytes=binding.source_verified_size_bytes
 and file.stored_sha256=binding.stored_verified_sha256 and file.size_bytes=binding.stored_verified_size_bytes
 from oss_upload_byte_bindings binding join oss_files file on file.id=binding.file_id
 where binding.id=$1::uuid and binding.file_id=$2 for update of binding,file`, *job.RawUploadBindingID, *job.FileID, job.Bucket, job.Endpoint, job.ObjectKey).
		Scan(&proof.BindingID, &proof.FileID, &proof.Owner, &proof.SourceSHA, &proof.SourceSize, &proof.StoredKey, &status, &exact)
	if errors.Is(err, pgx.ErrNoRows) {
		return proof, deferOSSRawDeletionTx(ctx, tx, job, "raw_receipt_unknown", nil)
	}
	if err != nil {
		return proof, err
	}
	if _, err := requireOSSRawDeletionLeaseTx(ctx, tx, job); err != nil {
		return proof, err
	}
	if status != "deleted" || !exact || proof.SourceSize <= 0 || proof.SourceSize > maxOSSUploadBytes || normalizeSHA256(proof.SourceSHA) != proof.SourceSHA || proof.SourceSHA == "" {
		return proof, deferOSSRawDeletionTx(ctx, tx, job, "raw_terminal_proof_unknown", nil)
	}
	var expiry *time.Time
	if err := tx.QueryRow(ctx, `select max(max_expires_at) from oss_upload_capabilities where bucket=$1 and endpoint=$2 and object_key=$3`, job.Bucket, job.Endpoint, job.ObjectKey).Scan(&expiry); err != nil {
		return proof, err
	}
	if expiry == nil {
		return proof, deferOSSRawDeletionTx(ctx, tx, job, "raw_capability_unknown", nil)
	}
	// Evaluate a new database clock statement after every advisory/row wait.
	var expired bool
	if err := tx.QueryRow(ctx, `select clock_timestamp()>$1::timestamptz+interval '1 second'`, *expiry).Scan(&expired); err != nil {
		return proof, err
	}
	if !expired {
		next := expiry.Add(time.Second)
		return proof, deferOSSRawDeletionTx(ctx, tx, job, "raw_capability_live", &next)
	}
	var shared, preparing, unsettled bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from oss_files where object_key=$1 and status<>'deleted'),exists(select 1 from oss_upload_byte_bindings binding where binding.bucket=$2 and binding.endpoint=$3 and binding.upload_object_key=$1 and binding.status='preparing' and binding.lease_expires_at>clock_timestamp()),exists(select 1 from oss_multipart_sessions where bucket=$2 and endpoint=$3 and object_key=$1 and status not in ('completed','aborted'))`, job.ObjectKey, job.Bucket, job.Endpoint).Scan(&shared, &preparing, &unsettled); err != nil {
		return proof, err
	}
	if shared || preparing || unsettled {
		return proof, deferOSSRawDeletionTx(ctx, tx, job, "raw_active_reference_or_attempt", nil)
	}
	var activeReceipt bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from oss_upload_byte_bindings binding join oss_files file on file.id=binding.file_id where binding.id<>$1::uuid and binding.status='committed' and file.status<>'deleted' and ((binding.bucket=$2 and binding.endpoint=$3 and binding.upload_object_key=$4) or binding.sealed_source_key=$4 or binding.stored_object_key=$4))`, proof.BindingID, job.Bucket, job.Endpoint, job.ObjectKey).Scan(&activeReceipt); err != nil {
		return proof, err
	}
	activeJob, err := catalogImportSourceHasActiveJob(ctx, tx, proof.FileID)
	if err != nil {
		return proof, err
	}
	var pendingProof bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from creator_claim_attachments a join creator_claims claim on claim.id=a.claim_id where a.oss_file_id=$1 and claim.status='pending') or exists(select 1 from minecraft_server_proof_files proof join minecraft_servers server on server.id=proof.server_id where proof.oss_file_id=$1 and server.review_status='pending') or exists(select 1 from project_editor_application_attachments a join project_editor_applications application on application.id=a.application_id where a.oss_file_id=$1 and application.status='pending')`, proof.FileID).Scan(&pendingProof); err != nil {
		return proof, err
	}
	if activeReceipt || activeJob || pendingProof {
		return proof, deferOSSRawDeletionTx(ctx, tx, job, "raw_active_business_reference", nil)
	}
	return proof, nil
}

func isOSSRawObjectAbsent(err error) bool {
	var failure *aliyunoss.ServiceError
	return errors.As(err, &failure) && failure.StatusCode == 404 && failure.Code == "NoSuchKey"
}

func (worker *OSSDeletionWorker) deleteOriginalUpload(ctx context.Context, job ossDeletionJob, cfg ossConfigPayload) error {
	if !cfg.Enabled || cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return errOSSDeletionCredentialsUnavailable
	}
	if job.Bucket == "" || job.Endpoint == "" || job.Region == "" {
		return errOSSDeletionTargetIncomplete
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	initial, err := worker.server.db.Begin(ctx)
	if err != nil {
		return err
	}
	proof, err := lockOSSDeletedRawProof(ctx, initial, job)
	var remaining time.Duration
	if err == nil {
		remaining, err = requireOSSRawDeletionLeaseTx(ctx, initial, job)
	}
	_ = initial.Rollback(ctx)
	if err != nil {
		return err
	}
	cfg.Bucket, cfg.Endpoint, cfg.Region = job.Bucket, job.Endpoint, job.Region
	client := newOSSClient(cfg, cfg.Endpoint, job.UseCName || isCustomOSSEndpoint(cfg.Endpoint))
	// Hash through the actual provider before the final critical section; do not
	// retain a database connection during a potentially large streaming read.
	readCtx, readCancel := context.WithTimeout(ctx, min(30*time.Second, remaining))
	err = verifyOSSUploadedSourceBytes(readCtx, client, cfg, job.ObjectKey, proof.SourceSize, proof.SourceSHA)
	readCancel()
	if err != nil && !isOSSRawObjectAbsent(err) {
		return fmt.Errorf("verify terminal original input: %w", err)
	}
	final, err := worker.server.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer final.Rollback(ctx)
	current, err := lockOSSDeletedRawProof(ctx, final, job)
	if err != nil {
		return err
	}
	if current != proof {
		return deferOSSRawDeletionTx(ctx, final, job, "raw_terminal_proof_changed", nil)
	}
	remaining, err = requireOSSRawDeletionLeaseTx(ctx, final, job)
	if err != nil {
		return err
	}
	providerCtx, providerCancel := context.WithTimeout(ctx, min(30*time.Second, remaining))
	defer providerCancel()
	absentMultipart, err := ossDeletedRawMultipartAbsent(providerCtx, client, job.Bucket, job.ObjectKey)
	if err != nil {
		return err
	}
	if !absentMultipart {
		return deferOSSRawDeletionTx(ctx, final, job, "raw_unsettled_provider_multipart", nil)
	}
	if _, err = requireOSSRawDeletionLeaseTx(ctx, final, job); err != nil {
		return err
	}
	// The lease and original-key mutex remain held through DELETE and the
	// exact provider absence check. No nested pool acquisition occurs here.
	if _, err := client.DeleteObject(providerCtx, &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(job.Bucket), Key: aliyunoss.Ptr(job.ObjectKey)}); err != nil {
		return err
	}
	response, absentErr := client.GetObject(providerCtx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(job.Bucket), Key: aliyunoss.Ptr(job.ObjectKey)})
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if !isOSSRawObjectAbsent(absentErr) {
		return errors.New("terminal original upload absence is not proved")
	}
	_, err = requireOSSRawDeletionLeaseTx(ctx, final, job)
	return err
}

// The caller already owns this queue row. Evaluate the current database clock
// in a new statement after all row/key waits, not in a pre-lock WHERE clause.
func requireOSSRawDeletionLeaseTx(ctx context.Context, tx pgx.Tx, job ossDeletionJob) (time.Duration, error) {
	var owned bool
	var seconds float64
	err := tx.QueryRow(ctx, `select status='processing' and locked_by=$2,coalesce(extract(epoch from(locked_at+make_interval(secs=>$3)-clock_timestamp())),0)::double precision from oss_object_deletion_outbox where id=$1`, job.ID, job.LockToken, int(ossDeletionLeaseTimeout/time.Second)).Scan(&owned, &seconds)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (!owned || seconds <= 0) {
		return 0, errOSSDeletionLeaseLost
	}
	if err != nil {
		return 0, err
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func (worker *OSSDeletionWorker) completeOriginalUpload(ctx context.Context, job ossDeletionJob) error {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `select id from oss_object_deletion_outbox where id=$1 and status='processing' and locked_by=$2 for update`, job.ID, job.LockToken).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return errOSSDeletionLeaseLost
	}
	if err != nil {
		return err
	}
	if _, err = requireOSSRawDeletionLeaseTx(ctx, tx, job); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `update oss_object_deletion_outbox set status='completed',deleted_at=clock_timestamp(),dead_at=null,locked_at=null,locked_by='',last_error='',failure_class='',raw_hold_reason='',updated_at=clock_timestamp() where id=$1 and status='processing' and locked_by=$2 and locked_at+make_interval(secs=>$3)>clock_timestamp()`, job.ID, job.LockToken, int(ossDeletionLeaseTimeout/time.Second))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errOSSDeletionLeaseLost
	}
	return tx.Commit(ctx)
}

// An object HEAD404 alone does not prove that a later multipart completion
// cannot recreate it. Never abort an unrecognized upload or widen a prefix to
// object ownership: retain the intent if an exact-key upload or an incomplete
// bounded listing remains. The normal completed-session path lists no upload.
func ossDeletedRawMultipartAbsent(ctx context.Context, client *aliyunoss.Client, bucket, key string) (bool, error) {
	var keyMarker, uploadMarker string
	for range 4 {
		result, err := client.ListMultipartUploads(ctx, &aliyunoss.ListMultipartUploadsRequest{Bucket: aliyunoss.Ptr(bucket), Prefix: aliyunoss.Ptr(key), KeyMarker: aliyunoss.Ptr(keyMarker), UploadIdMarker: aliyunoss.Ptr(uploadMarker), MaxUploads: 32})
		if err != nil {
			return false, err
		}
		for _, upload := range result.Uploads {
			if aliyunoss.ToString(upload.Key) == key {
				return false, nil
			}
		}
		if !result.IsTruncated {
			return true, nil
		}
		nextKey, nextUpload := aliyunoss.ToString(result.NextKeyMarker), aliyunoss.ToString(result.NextUploadIdMarker)
		if nextKey == "" || (nextKey == keyMarker && nextUpload == uploadMarker) {
			return false, nil
		}
		keyMarker, uploadMarker = nextKey, nextUpload
	}
	return false, nil
}
