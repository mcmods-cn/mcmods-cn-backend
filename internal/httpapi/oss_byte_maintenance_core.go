package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

var errOSSMaintenanceBusy = errors.New("maintenance action has a live owner")
var errOSSMaintenanceConflict = errors.New("maintenance action or file changed")
var errOSSMaintenanceDone = errors.New("maintenance action already completed")
var errOSSMaintenanceBudget = errors.New("maintenance byte budget exceeded")

type ossMaintenanceObject struct {
	Head *aliyunoss.HeadObjectResult
	SHA  string
	Size int64
}

func (run *ossMaintenanceRun) loadFile(ctx context.Context, publicID string) (ossMaintenanceFile, error) {
	var file ossMaintenanceFile
	tx, err := run.s.db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return file, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `select to_jsonb(file)::text from public.oss_files file where file.public_id=$1`, publicID).Scan(&file.snapshot); err != nil {
		return file, err
	}
	if err = json.Unmarshal([]byte(file.snapshot), &file); err != nil {
		return file, err
	}
	return file, tx.Commit(ctx)
}

func (run *ossMaintenanceRun) observe(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, key string) (ossMaintenanceObject, error) {
	var out ossMaintenanceObject
	head, err := client.HeadObject(ctx, &aliyunoss.HeadObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(key)})
	if err != nil {
		return out, err
	}
	if head.ContentLength < 1 || head.ContentLength > maxOSSUploadBytes || head.ContentLength+1 > run.plan.MaxReadBytes-run.read {
		return out, errOSSMaintenanceBudget
	}
	response, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(key)})
	if err != nil {
		return out, err
	}
	defer response.Body.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(response.Body, head.ContentLength+1))
	run.read += n
	if err != nil {
		return out, err
	}
	if n != head.ContentLength {
		return out, errors.New("actual object size changed while reading")
	}
	out.Head, out.Size, out.SHA = head, n, hex.EncodeToString(hash.Sum(nil))
	return out, nil
}

func (run *ossMaintenanceRun) references(ctx context.Context, file ossMaintenanceFile, key string, includeFKs bool) (ossMaintenanceReferences, error) {
	tx, err := run.s.db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return ossMaintenanceReferences{}, err
	}
	defer tx.Rollback(ctx)
	refs, err := inspectOSSByteMaintenanceReferences(ctx, tx, file.ID, key, run.plan, includeFKs)
	if err != nil {
		return refs, err
	}
	if err = tx.Commit(ctx); err != nil {
		refs.Complete = false
	}
	return refs, err
}

func (run *ossMaintenanceRun) claimAction(ctx context.Context, cfg ossConfigPayload, e OSSByteMaintenanceEntry, file ossMaintenanceFile, targetKey, sourceKey, initialState string) (string, error) {
	tx, err := run.s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	keys := []string{targetKey}
	if e.Operation == "promote" && sourceKey != targetKey {
		keys = append(keys, sourceKey)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('oss-completion:'||$1,0))`, cfg.Bucket+":"+cfg.Endpoint+":"+key); err != nil {
			return "", err
		}
	}
	if e.Operation == "promote" {
		var retired bool
		if err = tx.QueryRow(ctx, `select exists(select 1 from public.oss_upload_maintenance_actions where operation='cleanup_raw' and bucket=$1 and endpoint=$2 and target_key=any($3::text[]))`, cfg.Bucket, cfg.Endpoint, keys).Scan(&retired); err != nil {
			return "", err
		}
		if retired {
			return "", errOSSRawUploadRetired
		}
	}
	planSHA := ossMaintenancePlanSHA(e, run.plan)
	var oldSHA, state, oldSnapshot string
	err = tx.QueryRow(ctx, `select plan_sha256,state,file_preimage::text from public.oss_upload_maintenance_actions where id=$1::uuid for update`, e.ActionID).Scan(&oldSHA, &state, &oldSnapshot)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if err == nil {
		if oldSHA != planSHA || oldSnapshot != file.snapshot {
			return "", errOSSMaintenanceConflict
		}
		if state == "completed" {
			return "", errOSSMaintenanceDone
		}
		if state == "promoted" {
			return "", errOSSMaintenanceConflict
		}
		var live bool
		if err = tx.QueryRow(ctx, `select lease_expires_at>clock_timestamp() from public.oss_upload_maintenance_actions where id=$1::uuid`, e.ActionID).Scan(&live); err != nil {
			return "", err
		}
		if live && (state == "preparing" || state == "processing" || state == "cleanup_queued") {
			return "", errOSSMaintenanceBusy
		}
	}
	var exact int64
	if err = tx.QueryRow(ctx, `select id from public.oss_files file where id=$1 and to_jsonb(file)=$2::jsonb for update`, file.ID, file.snapshot).Scan(&exact); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errOSSMaintenanceConflict
		}
		return "", err
	}
	token := newExportID()
	_, err = tx.Exec(ctx, `insert into public.oss_upload_maintenance_actions(id,operation,state,file_id,bucket,endpoint,region,use_cname,target_key,source_key,plan_sha256,backup_reference,file_preimage,run_token,lease_expires_at,reference_schema_sha256,quiescence_declared,quiesced_reference_tables)
 values($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14,clock_timestamp()+interval '10 minutes',$15,$16,$17::text[])
 on conflict(id) do update set state=excluded.state,run_token=excluded.run_token,lease_expires_at=excluded.lease_expires_at,updated_at=clock_timestamp()`, e.ActionID, e.Operation, initialState, file.ID, cfg.Bucket, cfg.Endpoint, cfg.Region, cfg.UseCName || isCustomOSSEndpoint(cfg.Endpoint), targetKey, sourceKey, planSHA, run.opts.BackupReference, file.snapshot, token, run.plan.ReferenceSchemaSHA, run.plan.ReferenceWritesQuiesced, run.plan.QuiescedReferenceTables)
	if err != nil {
		return "", err
	}
	return token, tx.Commit(ctx)
}

func lockOSSByteMaintenanceActionTx(ctx context.Context, tx pgx.Tx, actionID, token, state string) error {
	var gotToken, gotState string
	if err := tx.QueryRow(ctx, `select run_token,state from public.oss_upload_maintenance_actions where id=$1::uuid for update`, actionID).Scan(&gotToken, &gotState); err != nil {
		return err
	}
	if gotToken != token || gotState != state {
		return errOSSByteBindingLeaseLost
	}
	var live bool
	if err := tx.QueryRow(ctx, `select lease_expires_at>clock_timestamp() from public.oss_upload_maintenance_actions where id=$1::uuid`, actionID).Scan(&live); err != nil {
		return err
	}
	if !live {
		return errOSSByteBindingLeaseLost
	}
	return nil
}

func (run *ossMaintenanceRun) failAction(actionID, token string) {
	run.failActionState(actionID, token, "preparing")
}

func (run *ossMaintenanceRun) failActionState(actionID, token, state string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := run.s.db.Begin(ctx)
	if err != nil {
		run.s.observeOSSWriteFailure("maintenance_failure_record", actionID, err)
		return // Durable expired preparation remains inspectable/retryable.
	}
	defer tx.Rollback(ctx)
	if lockOSSByteMaintenanceActionTx(ctx, tx, actionID, token, state) != nil {
		return
	}
	if _, err = tx.Exec(ctx, `update public.oss_upload_maintenance_actions set state='failed',updated_at=clock_timestamp() where id=$1::uuid`, actionID); err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		run.s.observeOSSWriteFailure("maintenance_failure_record", actionID, err)
	}
}

func ossMaintenanceErrorStatus(err error) string {
	if status := ossMaintenanceScopeStatus(err); status != "" {
		return status
	}
	if errors.Is(err, errOSSMaintenanceDone) {
		return "ALREADY_COMPLETED"
	}
	if errors.Is(err, errOSSRawUploadRetired) {
		return "RAW_INPUT_RETIRED"
	}
	if errors.Is(err, errOSSMaintenanceBusy) {
		return "BUSY"
	}
	if errors.Is(err, errOSSMaintenanceConflict) || errors.Is(err, errOSSByteBindingLeaseLost) {
		return "STATE_CONFLICT"
	}
	if errors.Is(err, errOSSMaintenanceBudget) {
		return "BYTE_BUDGET_EXCEEDED"
	}
	return "OPERATION_ERROR"
}
