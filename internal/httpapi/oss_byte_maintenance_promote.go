package httpapi

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

type ossMaintenanceTypedProof struct {
	ID, SourceKey, StoredKey, SourceSHA, StoredSHA, FileEndpoint string
	SourceSize, StoredSize                                       int64
	Owner                                                        int64
	Category, Source                                             string
}

func (run *ossMaintenanceRun) typedProof(ctx context.Context, file ossMaintenanceFile, cfg ossConfigPayload) (ossMaintenanceTypedProof, bool, error) {
	var proof ossMaintenanceTypedProof
	var present bool
	if err := run.s.db.QueryRow(ctx, `select to_regclass('public.oss_upload_byte_bindings') is not null`).Scan(&present); err != nil || !present {
		return proof, false, err
	}
	var mapping bool
	if err := run.s.db.QueryRow(ctx, `select exists(select 1 from pg_attribute where attrelid='public.oss_upload_byte_bindings'::regclass and attname='file_endpoint' and not attisdropped)`).Scan(&mapping); err != nil {
		return proof, false, err
	}
	endpoint := "''::text"
	if mapping {
		endpoint = "binding.file_endpoint"
	}
	err := run.s.db.QueryRow(ctx, `select binding.id::text,binding.sealed_source_key,binding.stored_object_key,binding.source_verified_sha256,binding.stored_verified_sha256,binding.source_verified_size_bytes,binding.stored_verified_size_bytes,binding.owner_id,binding.logical_category,binding.source,`+endpoint+`
 from public.oss_upload_byte_bindings binding where binding.file_id=$1 and binding.status='committed' and binding.bucket=$2 and binding.endpoint=$3 order by binding.created_at,binding.id limit 1`, file.ID, cfg.Bucket, cfg.Endpoint).Scan(&proof.ID, &proof.SourceKey, &proof.StoredKey, &proof.SourceSHA, &proof.StoredSHA, &proof.SourceSize, &proof.StoredSize, &proof.Owner, &proof.Category, &proof.Source, &proof.FileEndpoint)
	if errors.Is(err, pgx.ErrNoRows) {
		return proof, false, nil
	}
	return proof, err == nil, err
}

func (run *ossMaintenanceRun) promote(ctx context.Context, e OSSByteMaintenanceEntry) OSSByteMaintenanceResult {
	out := OSSByteMaintenanceResult{ActionID: e.ActionID, FilePublicID: e.FilePublicID, Operation: e.Operation}
	file, err := run.loadFile(ctx, e.FilePublicID)
	if errors.Is(err, pgx.ErrNoRows) {
		out.Status = "FILE_NOT_FOUND"
		return out
	}
	if err != nil {
		out.Status = "DATABASE_ERROR"
		return out
	}
	if file.Status != "active" || file.Owner == nil || *file.Owner <= 0 {
		out.Status = "INELIGIBLE_FILE_STATE"
		return out
	}
	if run.hasJournal {
		var priorSHA string
		var priorFile int64
		priorErr := run.s.db.QueryRow(ctx, `select plan_sha256,file_id from public.oss_upload_maintenance_actions where id=$1::uuid`, e.ActionID).Scan(&priorSHA, &priorFile)
		if priorErr != nil && !errors.Is(priorErr, pgx.ErrNoRows) {
			out.Status = "DATABASE_ERROR"
			return out
		}
		if priorErr == nil && (priorSHA != ossMaintenancePlanSHA(e, run.plan) || priorFile != file.ID) {
			out.Status = "PLAN_CONFLICT"
			return out
		}
	}
	client, cfg, err := run.s.ossClient(ctx)
	if err != nil {
		out.Status = "CONFIGURATION_UNAVAILABLE"
		return out
	}
	if file.Bucket != cfg.Bucket || file.Endpoint != cfg.displayEndpoint() || !isAllowedObjectKey(file.Key, cfg.Prefix) || (e.ExpectedDisplayEndpoint != "" && e.ExpectedDisplayEndpoint != file.Endpoint) {
		out.Status = "TARGET_LOCATION_MISMATCH"
		return out
	}
	proof, typed, err := run.typedProof(ctx, file, cfg)
	if err != nil {
		out.Status = "DATABASE_ERROR"
		return out
	}
	if typed {
		b := &ossUploadByteBinding{ID: proof.ID, SourceKey: proof.SourceKey}
		if proof.Owner != *file.Owner || proof.Category != file.Category || proof.Source != file.Source || !ownedOSSByteBindingCandidate(b, proof.SourceKey) || !ownedOSSByteBindingCandidate(b, proof.StoredKey) {
			out.Status = "UNPROVEN_TYPED_METADATA"
			return out
		}
	}
	if typed && file.Verified && proof.FileEndpoint == file.Endpoint && proof.StoredKey == file.Key && proof.SourceSHA == file.SHA && proof.StoredSHA == file.StoredSHA && proof.SourceSize == file.SourceSize && proof.StoredSize == file.Size {
		if (e.ExpectedSourceSHA != "" && normalizeSHA256(e.ExpectedSourceSHA) != proof.SourceSHA) || (e.ExpectedStoredSHA != "" && normalizeSHA256(e.ExpectedStoredSHA) != proof.StoredSHA) || (e.ExpectedSourceSize > 0 && e.ExpectedSourceSize != proof.SourceSize) || (e.ExpectedStoredSize > 0 && e.ExpectedStoredSize != proof.StoredSize) {
			out.Status = "PLAN_PROOF_MISMATCH"
		} else {
			out.Status = "ALREADY_VERIFIED"
		}
		return out
	}
	sourceKey, known := knownOSSLegacySourceKey(file, e.OriginalSourceKey)
	if typed {
		sourceKey, known = proof.SourceKey, true
	}
	if !known {
		out.Status = "MISSING_ORIGINAL_SOURCE"
		if e.OriginalSourceKey != "" {
			out.Status = "SOURCE_SCOPE_MISMATCH"
		}
		return out
	}
	if !isAllowedObjectKey(sourceKey, cfg.Prefix) {
		out.Status = "SOURCE_SCOPE_MISMATCH"
		return out
	}
	if !typed && isOSSSealedObjectKey(file.Key) {
		out.Status = "UNKNOWN_RESERVED_OBJECT_OWNERSHIP"
		return out
	}
	stored, err := run.observe(ctx, client, cfg, file.Key)
	if err != nil {
		out.Status = ossMaintenanceReadStatus(err, "MISSING_STORED_OBJECT")
		return out
	}
	source := stored
	if sourceKey != file.Key {
		source, err = run.observe(ctx, client, cfg, sourceKey)
		if err != nil {
			out.Status = ossMaintenanceReadStatus(err, "MISSING_ORIGINAL_SOURCE")
			return out
		}
	}
	out.SourceObservedSHA, out.StoredObservedSHA, out.SourceBytes, out.StoredBytes = source.SHA, stored.SHA, source.Size, stored.Size
	if (e.ExpectedSourceSHA != "" && normalizeSHA256(e.ExpectedSourceSHA) != source.SHA) || (e.ExpectedStoredSHA != "" && normalizeSHA256(e.ExpectedStoredSHA) != stored.SHA) || (e.ExpectedSourceSize > 0 && e.ExpectedSourceSize != source.Size) || (e.ExpectedStoredSize > 0 && e.ExpectedStoredSize != stored.Size) {
		out.Status = "BYTE_PIN_MISMATCH"
		return out
	}
	if source.Size != file.SourceSize || stored.Size != file.Size {
		out.Status = "NEEDS_QUOTA_SIZE_RECONCILIATION"
		return out
	}
	if typed {
		if proof.FileEndpoint != "" || proof.StoredKey != file.Key || proof.SourceSHA != source.SHA || proof.StoredSHA != stored.SHA || proof.SourceSize != source.Size || proof.StoredSize != stored.Size || file.SHA != source.SHA || file.StoredSHA != stored.SHA {
			out.Status = "UNPROVEN_TYPED_METADATA"
			return out
		}
		if !run.opts.Apply {
			out.Status = "READY_TYPED_ENDPOINT_RESTORE"
			return out
		}
		err = run.restoreTypedEndpoint(ctx, e, file, cfg, proof)
		if err != nil {
			out.Status = ossMaintenanceErrorStatus(err)
		} else {
			out.Status = "RESTORED_TYPED_ENDPOINT"
		}
		return out
	}
	refs, err := run.references(ctx, file, file.Key, true)
	out.ReferenceRows, out.ReferencesKnown = refs.Rows, refs.Complete
	if status := ossMaintenanceReferencesStatus(refs, err); status != "" {
		out.Status = status
		return out
	}
	if !run.opts.Apply {
		out.Status = "READY_PINNED_PROMOTION"
		return out
	}
	token, err := run.claimAction(ctx, cfg, e, file, file.Key, sourceKey, "preparing")
	if err != nil {
		out.Status = ossMaintenanceErrorStatus(err)
		return out
	}
	success := false
	defer func() {
		if !success {
			run.failAction(e.ActionID, token)
		}
	}()
	b := &ossUploadByteBinding{ID: newExportID(), Token: newExportID(), UploadKey: file.Key, cfg: cfg}
	b.SourceKey = path.Join(path.Dir(file.Key), ossSealedUploadComponent, b.ID, path.Base(sourceKey))
	b.StoredKey = b.SourceKey
	name := file.SourceOriginalName
	if name == "" {
		name = file.OriginalName
	}
	req := ossCompleteUploadRequest{ObjectKey: file.Key, OriginalName: name, Category: file.Category, Source: file.Source, SHA256: source.SHA, SizeBytes: source.Size}
	if err = run.s.persistOSSByteBindingPreparation(ctx, b, req, *file.Owner, source.Size); err != nil {
		out.Status = ossMaintenanceErrorStatus(err)
		return out
	}
	b.ctx, b.stop = run.s.startOSSByteBindingHeartbeat(ctx, b)
	defer run.s.discardOSSByteBinding(b)
	if err = copyOSSUploadToSealedKey(b.ctx, client, cfg, sourceKey, b.SourceKey, source.Head); err != nil {
		out.Status = "PROVIDER_ERROR"
		return out
	}
	verified, err := run.observe(b.ctx, client, cfg, b.SourceKey)
	if err != nil || verified.SHA != source.SHA || verified.Size != source.Size {
		out.Status = "SEALED_SOURCE_MISMATCH"
		return out
	}
	if sourceKey != file.Key {
		destination := path.Join(path.Dir(b.SourceKey), "stored-"+path.Base(file.Key))
		if err = run.s.planOSSByteBindingStoredKey(b, destination); err != nil {
			out.Status = ossMaintenanceErrorStatus(err)
			return out
		}
		if err = copyOSSUploadToSealedKey(b.ctx, client, cfg, file.Key, destination, stored.Head); err != nil {
			out.Status = "PROVIDER_ERROR"
			return out
		}
		actual, readErr := run.observe(b.ctx, client, cfg, destination)
		if readErr != nil || actual.SHA != stored.SHA || actual.Size != stored.Size {
			out.Status = "SEALED_STORED_MISMATCH"
			return out
		}
	}
	b.stop()
	err = run.adoptLegacy(ctx, e, token, file, cfg, b, source, stored)
	if err != nil {
		out.Status = ossMaintenanceErrorStatus(err)
		return out
	}
	success = true
	out.Status = "PROMOTED"
	return out
}

func ossMaintenanceReadStatus(err error, missing string) string {
	if errors.Is(err, errOSSMaintenanceBudget) {
		return "BYTE_BUDGET_EXCEEDED"
	}
	var service *aliyunoss.ServiceError
	if errors.As(err, &service) && (service.Code == "NoSuchKey" || (service.StatusCode == 404 && service.Code == "BadErrorResponse")) {
		return missing
	}
	return "PROVIDER_ERROR"
}

func (run *ossMaintenanceRun) adoptLegacy(ctx context.Context, e OSSByteMaintenanceEntry, token string, file ossMaintenanceFile, cfg ossConfigPayload, b *ossUploadByteBinding, source, stored ossMaintenanceObject) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := run.s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	acquire, cancelAcquire := context.WithTimeout(ctx, 5*time.Second)
	defer cancelAcquire()
	if _, err = tx.Exec(acquire, `select pg_advisory_xact_lock(hashtextextended('oss-completion:'||$1,0))`, cfg.Bucket+":"+cfg.Endpoint+":"+file.Key); err != nil {
		return err
	}
	fencedTables, err := run.freezeReferenceWriteScope(acquire, tx)
	if err != nil {
		return err
	}
	if err = lockOSSByteBindingTx(acquire, tx, b); err != nil {
		return err
	}
	if err = lockOSSByteMaintenanceActionTx(acquire, tx, e.ActionID, token, "preparing"); err != nil {
		return err
	}
	var locked int64
	if err = tx.QueryRow(acquire, `select id from public.oss_files file where id=$1 and to_jsonb(file)=$2::jsonb for update`, file.ID, file.snapshot).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errOSSMaintenanceConflict
		}
		return err
	}
	cancelAcquire()
	refs, err := inspectOSSByteMaintenanceReferences(ctx, tx, file.ID, file.Key, run.plan, true)
	if err != nil {
		return err
	}
	if !refs.Complete || refs.Rows > 0 {
		return errOSSMaintenanceConflict
	}
	if _, err = tx.Exec(ctx, `update public.oss_files set object_key=$2,sha256=$3,stored_sha256=$4,source_hash_verified=true,updated_at=clock_timestamp() where id=$1`, file.ID, b.StoredKey, source.SHA, stored.SHA); err != nil {
		return err
	}
	if err = commitOSSByteBindingTx(ctx, tx, b, file.ID, source.SHA, source.Size, stored.SHA, stored.Size); err != nil {
		return err
	}
	// File updates and reference work may cross the action expiry. Recheck
	// the current DB clock before marking this owner terminal.
	if err = lockOSSByteMaintenanceActionTx(ctx, tx, e.ActionID, token, "preparing"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update public.oss_upload_maintenance_actions set state='promoted',binding_id=$2::uuid,write_fence_tables=$3::text[],updated_at=clock_timestamp() where id=$1::uuid`, e.ActionID, b.ID, fencedTables); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (run *ossMaintenanceRun) restoreTypedEndpoint(ctx context.Context, e OSSByteMaintenanceEntry, file ossMaintenanceFile, cfg ossConfigPayload, proof ossMaintenanceTypedProof) error {
	if e.ExpectedDisplayEndpoint != file.Endpoint || strings.TrimSpace(proof.FileEndpoint) != "" {
		return errOSSMaintenanceConflict
	}
	token, err := run.claimAction(ctx, cfg, e, file, file.Key, proof.SourceKey, "preparing")
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			run.failAction(e.ActionID, token)
		}
	}()
	tx, err := run.s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('oss-completion:'||$1,0))`, cfg.Bucket+":"+cfg.Endpoint+":"+file.Key); err != nil {
		return err
	}
	var binding string
	if err = tx.QueryRow(ctx, `select id::text from public.oss_upload_byte_bindings where id=$1::uuid and file_id=$2 and status='committed' and file_endpoint='' and sealed_source_key=$3 and stored_object_key=$4 and source_verified_sha256=$5 and stored_verified_sha256=$6 for update`, proof.ID, file.ID, proof.SourceKey, proof.StoredKey, proof.SourceSHA, proof.StoredSHA).Scan(&binding); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errOSSMaintenanceConflict
		}
		return err
	}
	if err = lockOSSByteMaintenanceActionTx(ctx, tx, e.ActionID, token, "preparing"); err != nil {
		return err
	}
	var locked int64
	if err = tx.QueryRow(ctx, `select id from public.oss_files file where id=$1 and to_jsonb(file)=$2::jsonb for update`, file.ID, file.snapshot).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errOSSMaintenanceConflict
		}
		return err
	}
	if _, err = tx.Exec(ctx, `update public.oss_upload_byte_bindings set file_endpoint=$2,updated_at=clock_timestamp() where id=$1::uuid`, proof.ID, e.ExpectedDisplayEndpoint); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update public.oss_files set source_hash_verified=true where id=$1`, file.ID); err != nil {
		return err
	}
	// File updates and reference work may cross the action expiry. Recheck
	// the current DB clock before marking this owner terminal.
	if err = lockOSSByteMaintenanceActionTx(ctx, tx, e.ActionID, token, "preparing"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update public.oss_upload_maintenance_actions set state='promoted',binding_id=$2::uuid,updated_at=clock_timestamp() where id=$1::uuid`, e.ActionID, proof.ID); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	done = true
	return nil
}
