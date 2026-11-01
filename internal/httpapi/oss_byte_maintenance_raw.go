package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

type ossMaintenanceRawProof struct {
	ID, Key, SourceKey, StoredKey, SourceSHA, StoredSHA string
	SourceSize, StoredSize                              int64
	Created                                             time.Time
}

func (run *ossMaintenanceRun) loadRawProof(ctx context.Context, e OSSByteMaintenanceEntry, cfg ossConfigPayload) (ossMaintenanceFile, ossMaintenanceRawProof, error) {
	var file ossMaintenanceFile
	var proof ossMaintenanceRawProof
	err := run.s.db.QueryRow(ctx, `select to_jsonb(file)::text,binding.id::text,binding.upload_object_key,binding.sealed_source_key,binding.stored_object_key,binding.source_verified_sha256,binding.stored_verified_sha256,binding.source_verified_size_bytes,binding.stored_verified_size_bytes,binding.created_at
 from public.oss_upload_byte_bindings binding join public.oss_files file on file.id=binding.file_id where binding.id=$1::uuid and binding.bucket=$2 and binding.endpoint=$3 and file.status='active' and file.source_hash_verified and `+ossByteBindingCurrentFileProofSQL, e.BindingID, cfg.Bucket, cfg.Endpoint).Scan(&file.snapshot, &proof.ID, &proof.Key, &proof.SourceKey, &proof.StoredKey, &proof.SourceSHA, &proof.StoredSHA, &proof.SourceSize, &proof.StoredSize, &proof.Created)
	if err == nil {
		err = json.Unmarshal([]byte(file.snapshot), &file)
	}
	return file, proof, err
}

func (run *ossMaintenanceRun) cleanupRaw(ctx context.Context, e OSSByteMaintenanceEntry) OSSByteMaintenanceResult {
	out := OSSByteMaintenanceResult{ActionID: e.ActionID, Operation: e.Operation}
	client, cfg, err := run.s.ossClient(ctx)
	if err != nil {
		out.Status = "CONFIGURATION_UNAVAILABLE"
		return out
	}
	file, proof, err := run.loadRawProof(ctx, e, cfg)
	if errors.Is(err, pgx.ErrNoRows) {
		out.Status = "INELIGIBLE_RECEIPT"
		return out
	}
	if err != nil {
		out.Status = "DATABASE_ERROR"
		return out
	}
	out.FilePublicID = file.PublicID
	legacyOriginal := e.RawSourceKind == "legacy_original"
	if legacyOriginal {
		if !run.hasJournal {
			out.Status = "NO_RECORDED_LEGACY_SOURCE"
			return out
		}
		var source string
		if err = run.s.db.QueryRow(ctx, `select source_key from public.oss_upload_maintenance_actions where operation='promote' and state='promoted' and binding_id=$1::uuid and source_key<>target_key order by created_at,id limit 1`, proof.ID).Scan(&source); err != nil {
			out.Status = "NO_RECORDED_LEGACY_SOURCE"
			return out
		}
		proof.Key = source
	}
	var oldState, oldSHA string
	if run.hasJournal {
		err = run.s.db.QueryRow(ctx, `select state,plan_sha256 from public.oss_upload_maintenance_actions where id=$1::uuid`, e.ActionID).Scan(&oldState, &oldSHA)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			out.Status = "DATABASE_ERROR"
			return out
		}
		if err == nil && oldSHA != ossMaintenancePlanSHA(e, run.plan) {
			out.Status = "PLAN_CONFLICT"
			return out
		}
		if oldState == "completed" {
			out.Status = "ALREADY_COMPLETED"
			return out
		}
	}
	if isOSSSealedObjectKey(proof.Key) || !isAllowedObjectKey(proof.Key, cfg.Prefix) || proof.Key == proof.SourceKey || proof.Key == proof.StoredKey {
		out.Status = "INELIGIBLE_RAW_KEY"
		return out
	}
	tx, err := run.s.db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		out.Status = "DATABASE_ERROR"
		return out
	}
	status, refs, guardErr := run.rawGuards(ctx, tx, file, proof, cfg)
	_ = tx.Rollback(ctx)
	out.ReferenceRows, out.ReferencesKnown = refs.Rows, refs.Complete
	if guardErr != nil {
		out.Status = "REFERENCE_QUERY_ERROR"
		return out
	}
	if status != "" {
		out.Status = status
		return out
	}
	// Promotion of a converted legacy object aliases its previous stored key;
	// ordinary upload aliases represent the original source. Keep both proofs
	// explicit so compensation never substitutes PNG for old WebP, or vice versa.
	compensationKey, expectedSHA, expectedSize := proof.SourceKey, proof.SourceSHA, proof.SourceSize
	var legacyStored bool
	if run.hasJournal && !legacyOriginal {
		if err = run.s.db.QueryRow(ctx, `select exists(select 1 from public.oss_upload_maintenance_actions where operation='promote' and state='promoted' and binding_id=$1::uuid and target_key=$2 and source_key<>target_key)`, proof.ID, proof.Key).Scan(&legacyStored); err != nil {
			out.Status = "DATABASE_ERROR"
			return out
		}
	}
	if legacyStored {
		compensationKey, expectedSHA, expectedSize = proof.StoredKey, proof.StoredSHA, proof.StoredSize
	}
	backup, err := run.observe(ctx, client, cfg, compensationKey)
	if err != nil || backup.SHA != expectedSHA || backup.Size != expectedSize {
		out.Status = "RETAINED_BYTE_PROOF_UNAVAILABLE"
		return out
	}
	raw, err := run.observe(ctx, client, cfg, proof.Key)
	alreadyAbsent := false
	if err != nil {
		status := ossMaintenanceReadStatus(err, "ALREADY_ABSENT")
		if status != "ALREADY_ABSENT" || !run.opts.Apply {
			out.Status = status
			return out
		}
		alreadyAbsent = true
	}
	if !alreadyAbsent {
		if raw.SHA != expectedSHA || raw.Size != expectedSize {
			out.Status = "RAW_BYTE_IDENTITY_MISMATCH"
			return out
		}
		out.SourceObservedSHA, out.SourceBytes = raw.SHA, raw.Size
	}
	if !run.opts.Apply {
		out.Status = "READY_RAW_RETENTION_CLEANUP"
		return out
	}
	token, err := run.claimAction(ctx, cfg, e, file, proof.Key, compensationKey, "processing")
	if err != nil {
		out.Status = ossMaintenanceErrorStatus(err)
		return out
	}
	err = run.executeRawCleanup(ctx, e, token, file, proof, cfg, client)
	if err != nil {
		run.failActionState(e.ActionID, token, "processing")
		out.Status = ossMaintenanceErrorStatus(err)
	} else {
		out.Status = "RAW_REMOVED"
		if alreadyAbsent {
			out.Status = "RAW_ABSENCE_RECORDED"
		}
	}
	return out
}

func (run *ossMaintenanceRun) rawGuards(ctx context.Context, tx pgx.Tx, file ossMaintenanceFile, proof ossMaintenanceRawProof, cfg ossConfigPayload) (string, ossMaintenanceReferences, error) {
	var cutoffOK, registered, preparing, reserved, multipart, promotion bool
	promotionGuard := "false"
	if run.hasJournal {
		promotionGuard = `exists(select 1 from public.oss_upload_maintenance_actions where operation='promote' and state='preparing' and bucket=$4 and endpoint=$5 and source_key=$3)`
	}
	err := tx.QueryRow(ctx, `select $1::timestamptz<=clock_timestamp()-interval '7 days' and $2::timestamptz<=$1,
 exists(select 1 from public.oss_files where object_key=$3 and status<>'deleted'),
 exists(select 1 from public.oss_upload_byte_bindings where bucket=$4 and endpoint=$5 and upload_object_key=$3 and status in ('preparing','cleanup_pending')),
 exists(select 1 from public.oss_user_upload_quota_reservations where object_key=$3),
 exists(select 1 from public.oss_multipart_sessions where bucket=$4 and endpoint=$5 and object_key=$3 and status not in ('completed','aborted')),
 `+promotionGuard, run.plan.RetentionCutoff, proof.Created, proof.Key, cfg.Bucket, cfg.Endpoint).Scan(&cutoffOK, &registered, &preparing, &reserved, &multipart, &promotion)
	if err != nil {
		return "", ossMaintenanceReferences{}, err
	}
	if !cutoffOK {
		return "RETENTION_CUTOFF_NOT_MET", ossMaintenanceReferences{}, nil
	}
	if registered {
		return "REGISTERED_RAW_OBJECT", ossMaintenanceReferences{}, nil
	}
	if preparing || promotion {
		return "RAW_INPUT_IN_USE", ossMaintenanceReferences{}, nil
	}
	if reserved {
		return "RAW_QUOTA_RESERVATION_PRESENT", ossMaintenanceReferences{}, nil
	}
	if multipart {
		return "RAW_MULTIPART_UNSETTLED", ossMaintenanceReferences{}, nil
	}
	refs, err := inspectOSSByteMaintenanceReferences(ctx, tx, file.ID, proof.Key, run.plan, false)
	if status := ossMaintenanceReferencesStatus(refs, err); status != "" {
		return status, refs, err
	}
	status, err := ossMaintenanceRawCapabilityStatus(ctx, tx, cfg, proof.Key)
	return status, refs, err
}

// The receipt's seven-day retention cutoff does not prove that an SDK-issued
// write authorization expired. Unknown historical grants stay held. The apply
// path calls this again under the same original-key mutex used by every issuer,
// after its schema, action, receipt, file and reference-fence lock waits.
func ossMaintenanceRawCapabilityStatus(ctx context.Context, tx pgx.Tx, cfg ossConfigPayload, key string) (string, error) {
	var present bool
	if err := tx.QueryRow(ctx, `select to_regclass('public.oss_upload_capabilities') is not null`).Scan(&present); err != nil {
		return "", err
	}
	if !present {
		return "RAW_CAPABILITY_UNKNOWN_HOLD", nil
	}
	var expiry *time.Time
	if err := tx.QueryRow(ctx, `select max(max_expires_at) from public.oss_upload_capabilities where bucket=$1 and endpoint=$2 and object_key=$3`, cfg.Bucket, cfg.Endpoint, key).Scan(&expiry); err != nil {
		return "", err
	}
	if expiry == nil {
		return "RAW_CAPABILITY_UNKNOWN_HOLD", nil
	}
	var expired bool
	if err := tx.QueryRow(ctx, `select clock_timestamp()>$1::timestamptz+interval '1 second'`, *expiry).Scan(&expired); err != nil {
		return "", err
	}
	if !expired {
		return "RAW_CAPABILITY_LIVE_HOLD", nil
	}
	return "", nil
}

// This is the dedicated executor for already journaled scoped cleanup intents.
// It retains the original-identity mutex and current receipt/file/action locks
// through DELETE+HEAD, exactly as the existing deletion worker fences its own
// provider side effect. No helper acquires a second pool connection inside it.
func (run *ossMaintenanceRun) executeRawCleanup(ctx context.Context, e OSSByteMaintenanceEntry, token string, file ossMaintenanceFile, proof ossMaintenanceRawProof, cfg ossConfigPayload, client *aliyunoss.Client) error {
	ctx, cancelCritical := context.WithTimeout(ctx, 30*time.Second)
	defer cancelCritical()
	tx, err := run.s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	acquire, cancelAcquire := context.WithTimeout(ctx, 5*time.Second)
	defer cancelAcquire()
	if _, err = tx.Exec(acquire, `select pg_advisory_xact_lock(hashtextextended('oss-completion:'||$1,0))`, cfg.Bucket+":"+cfg.Endpoint+":"+proof.Key); err != nil {
		return err
	}
	fencedTables, err := run.freezeReferenceWriteScope(acquire, tx)
	if err != nil {
		return err
	}
	var binding string
	if err = tx.QueryRow(acquire, `select id::text from public.oss_upload_byte_bindings where id=$1::uuid and file_id=$2 and status='committed' for share`, proof.ID, file.ID).Scan(&binding); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errOSSMaintenanceConflict
		}
		return err
	}
	if err = lockOSSByteMaintenanceActionTx(acquire, tx, e.ActionID, token, "processing"); err != nil {
		return err
	}
	var fileID int64
	if err = tx.QueryRow(acquire, `select id from public.oss_files file where id=$1 and to_jsonb(file)=$2::jsonb for update`, file.ID, file.snapshot).Scan(&fileID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errOSSMaintenanceConflict
		}
		return err
	}
	cancelAcquire()
	var valid bool
	if err = tx.QueryRow(ctx, `select exists(select 1 from public.oss_files file join public.oss_upload_byte_bindings binding on binding.file_id=file.id where binding.id=$1::uuid and binding.bucket=$2 and binding.endpoint=$3 and file.status='active' and file.source_hash_verified and `+ossByteBindingCurrentFileProofSQL+`)`, proof.ID, cfg.Bucket, cfg.Endpoint).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return errOSSMaintenanceConflict
	}
	status, _, err := run.rawGuards(ctx, tx, file, proof, cfg)
	if err != nil {
		return err
	}
	if status != "" {
		if status == "RAW_CAPABILITY_UNKNOWN_HOLD" || status == "RAW_CAPABILITY_LIVE_HOLD" {
			return &ossMaintenanceScopeError{status}
		}
		return errOSSMaintenanceConflict
	}
	// Reference inventory has its own SQL deadline; provider IO gets a bounded
	// context and does not re-enter the database pool.
	remote, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err = client.DeleteObject(remote, &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(proof.Key)}); err != nil {
		return err
	}
	_, err = client.HeadObject(remote, &aliyunoss.HeadObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(proof.Key)})
	var service *aliyunoss.ServiceError
	if !errors.As(err, &service) || !(service.Code == "NoSuchKey" || (service.StatusCode == 404 && service.Code == "BadErrorResponse")) {
		return errors.New("raw object absence was not established")
	}
	if err = lockOSSByteMaintenanceActionTx(ctx, tx, e.ActionID, token, "processing"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update public.oss_upload_maintenance_actions set state='completed',binding_id=$2::uuid,write_fence_tables=$3::text[],updated_at=clock_timestamp() where id=$1::uuid`, e.ActionID, proof.ID, fencedTables); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
