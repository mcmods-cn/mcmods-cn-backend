package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

const ossByteBindingCleanupRetry = time.Minute
const ossByteBindingDiscardedRecheck = time.Hour

// Provider cancellation is not proof that a remote copy cannot finish late.
// Keep discarded receipts in a bounded, indexed reconciliation schedule. A
// currently absent private candidate can be queued again if it appears later.
func (s *Server) reconcileOSSByteBindingCleanup(ctx context.Context) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	b := &ossUploadByteBinding{}
	err = tx.QueryRow(ctx, `select id::text,run_token,sealed_source_key,stored_object_key,bucket,endpoint,region,use_cname
 from oss_upload_byte_bindings where status in ('cleanup_pending','discarded') and file_id is null and lease_expires_at<=clock_timestamp()
 order by lease_expires_at,id for update skip locked limit 1`).Scan(&b.ID, &b.Token, &b.SourceKey, &b.StoredKey, &b.cfg.Bucket, &b.cfg.Endpoint, &b.cfg.Region, &b.cfg.UseCName)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	keys := []string{b.SourceKey}
	if b.StoredKey != "" && b.StoredKey != b.SourceKey {
		keys = append(keys, b.StoredKey)
	}
	for _, key := range keys {
		if !ownedOSSByteBindingCandidate(b, key) {
			return false, errors.New("reconciliation target is outside the recorded attempt")
		}
	}
	var completed bool
	if err = tx.QueryRow(ctx, `select count(*)=$4 and coalesce(bool_and(status='completed'),false) from oss_object_deletion_outbox where bucket=$1 and endpoint=$2 and object_key=any($3::text[])`, b.cfg.Bucket, b.cfg.Endpoint, keys, len(keys)).Scan(&completed); err != nil {
		return false, err
	}
	if !completed {
		// Leave pending/dead deletion jobs observable. Never call an arbitrary
		// provider failure or a missing deletion row a completed compensation.
		if _, err = tx.Exec(ctx, `update oss_upload_byte_bindings set lease_expires_at=clock_timestamp()+make_interval(secs=>$2),updated_at=clock_timestamp() where id=$1::uuid`, b.ID, int(ossByteBindingCleanupRetry/time.Second)); err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	}
	var claimDeadline time.Time
	if err = tx.QueryRow(ctx, `update oss_upload_byte_bindings set lease_expires_at=clock_timestamp()+make_interval(secs=>$2),updated_at=clock_timestamp() where id=$1::uuid returning lease_expires_at`, b.ID, int(ossByteBindingLease/time.Second)).Scan(&claimDeadline); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	// No database transaction or pool connection is retained during HEAD.
	inspectCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cfg, cfgErr := s.ossConfigFromSettingsStrictWithQueryer(inspectCtx, s.db)
	var present bool
	if cfgErr == nil && (!cfg.Enabled || cfg.AccessKeyID == "" || cfg.AccessKeySecret == "") {
		cfgErr = errOSSDeletionCredentialsUnavailable
	}
	if cfgErr == nil {
		cfg.Bucket, cfg.Endpoint, cfg.Region, cfg.UseCName = b.cfg.Bucket, b.cfg.Endpoint, b.cfg.Region, b.cfg.UseCName
		client := newOSSClient(cfg, cfg.Endpoint, cfg.UseCName || isCustomOSSEndpoint(cfg.Endpoint))
		for _, key := range keys {
			_, headErr := client.HeadObject(inspectCtx, &aliyunoss.HeadObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(key)})
			if headErr == nil {
				present = true
				continue
			}
			var serviceErr *aliyunoss.ServiceError
			if errors.As(headErr, &serviceErr) && (serviceErr.Code == "NoSuchKey" || (serviceErr.StatusCode == 404 && serviceErr.Code == "BadErrorResponse")) {
				continue
			}
			cfgErr = headErr
			break
		}
	}
	finish, err := s.db.Begin(ctx)
	if err != nil {
		return true, err
	}
	defer finish.Rollback(ctx)
	var token, status string
	var deadline time.Time
	err = finish.QueryRow(ctx, `select run_token,status,lease_expires_at from oss_upload_byte_bindings where id=$1::uuid and file_id is null for update`, b.ID).Scan(&token, &status, &deadline)
	if err != nil {
		return true, err
	}
	if token != b.Token || !deadline.Equal(claimDeadline) || (status != "cleanup_pending" && status != "discarded") {
		return true, errOSSByteBindingLeaseLost
	}
	var live bool
	if err = finish.QueryRow(ctx, `select lease_expires_at>clock_timestamp() from oss_upload_byte_bindings where id=$1::uuid`, b.ID).Scan(&live); err != nil {
		return true, err
	}
	if !live {
		return true, errOSSByteBindingLeaseLost
	}
	next := ossByteBindingDiscardedRecheck
	nextStatus := "discarded"
	if cfgErr != nil || present {
		next = ossByteBindingCleanupRetry
		nextStatus = "cleanup_pending"
	}
	if cfgErr == nil && present {
		if err = queueOSSByteBindingCleanupTx(ctx, finish, b); err != nil {
			return true, err
		}
	}
	if _, err = finish.Exec(ctx, `update oss_upload_byte_bindings set status=$2,lease_expires_at=clock_timestamp()+make_interval(secs=>$3),updated_at=clock_timestamp() where id=$1::uuid`, b.ID, nextStatus, int(next/time.Second)); err != nil {
		return true, err
	}
	if err = finish.Commit(ctx); err != nil {
		return true, err
	}
	if cfgErr != nil {
		return true, fmt.Errorf("inspect private byte-binding candidate: %w", cfgErr)
	}
	return true, nil
}
