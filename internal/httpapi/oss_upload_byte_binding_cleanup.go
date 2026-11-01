package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func queueOSSByteBindingCleanupTx(ctx context.Context, tx pgx.Tx, b *ossUploadByteBinding) error {
	keys := []string{b.SourceKey}
	if b.StoredKey != "" && b.StoredKey != b.SourceKey {
		keys = append(keys, b.StoredKey)
	}
	for _, key := range keys {
		if !ownedOSSByteBindingCandidate(b, key) {
			return errors.New("refuse byte-binding cleanup outside reserved attempt")
		}
		if err := enqueueOSSObjectDeletionTx(ctx, tx, ossDeletionTarget{Bucket: b.cfg.Bucket, Endpoint: b.cfg.Endpoint, Region: b.cfg.Region, UseCName: b.cfg.UseCName, ObjectKey: key, Reason: "upload-byte-binding-not-adopted"}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) discardOSSByteBinding(b *ossUploadByteBinding) {
	if b == nil {
		return
	}
	if b.stop != nil {
		b.stop()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		s.observeOSSWriteFailure("byte_binding_cleanup", b.UploadKey, err)
		return
	}
	defer tx.Rollback(ctx)
	var status, token string
	err = tx.QueryRow(ctx, `select status,run_token,stored_object_key from oss_upload_byte_bindings where id=$1::uuid for update`, b.ID).Scan(&status, &token, &b.StoredKey)
	if err == nil && status == "committed" {
		return
	}
	if err == nil && token != b.Token {
		err = errOSSByteBindingLeaseLost
	}
	if err == nil {
		_, err = tx.Exec(ctx, `update oss_upload_byte_bindings set status='cleanup_pending',lease_expires_at=clock_timestamp()+interval '1 minute',updated_at=clock_timestamp() where id=$1::uuid`, b.ID)
	}
	if err == nil {
		err = queueOSSByteBindingCleanupTx(ctx, tx, b)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		s.observeOSSWriteFailure("byte_binding_cleanup", b.UploadKey, err)
	}
}

// Recovery uses the same receipt row lock as adoption. Only expired unadopted
// attempts are queued; the deletion worker still protects active shared files.
func (s *Server) recoverOSSByteBindingAttempt(ctx context.Context) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	b := &ossUploadByteBinding{}
	err = tx.QueryRow(ctx, `select id::text,run_token,sealed_source_key,stored_object_key,bucket,endpoint,region,use_cname from oss_upload_byte_bindings where status='preparing' and lease_expires_at<=clock_timestamp() order by lease_expires_at,id for update skip locked limit 1`).Scan(&b.ID, &b.Token, &b.SourceKey, &b.StoredKey, &b.cfg.Bucket, &b.cfg.Endpoint, &b.cfg.Region, &b.cfg.UseCName)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `update oss_upload_byte_bindings set status='cleanup_pending',lease_expires_at=clock_timestamp()+interval '1 minute',updated_at=clock_timestamp() where id=$1::uuid`, b.ID); err != nil {
		return false, err
	}
	if err = queueOSSByteBindingCleanupTx(ctx, tx, b); err != nil {
		return false, fmt.Errorf("queue expired byte-binding candidate: %w", err)
	}
	return true, tx.Commit(ctx)
}

// Source verification bytes remain private while their converted file is live.
// File deletion owns both its public stored object and these exclusive sources;
// keep shared active-file protection by leaving FileID nil for source cleanup.
func queueDeletedOSSByteBindingSourcesTx(ctx context.Context, tx pgx.Tx, fileID int64) error {
	rows, err := tx.Query(ctx, `select id::text,sealed_source_key,stored_object_key,bucket,endpoint,region,use_cname from oss_upload_byte_bindings where file_id=$1 and status='committed' and sealed_source_key<>stored_object_key`, fileID)
	if err != nil {
		return err
	}
	var sources []*ossUploadByteBinding
	for rows.Next() {
		b := &ossUploadByteBinding{}
		if err = rows.Scan(&b.ID, &b.SourceKey, &b.StoredKey, &b.cfg.Bucket, &b.cfg.Endpoint, &b.cfg.Region, &b.cfg.UseCName); err != nil {
			rows.Close()
			return err
		}
		sources = append(sources, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, b := range sources {
		if !ownedOSSByteBindingCandidate(b, b.SourceKey) {
			return errors.New("deleted file source is outside its recorded byte-binding attempt")
		}
		if err = enqueueOSSObjectDeletionTx(ctx, tx, ossDeletionTarget{Bucket: b.cfg.Bucket, Endpoint: b.cfg.Endpoint, Region: b.cfg.Region, UseCName: b.cfg.UseCName, ObjectKey: b.SourceKey, Reason: "deleted-upload-sealed-source"}); err != nil {
			return err
		}
	}
	return nil
}
