package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// This is the latest real SDK-issued write deadline, not source ownership or
// permission. No signed URL, header, token or provider credential is retained.
type ossUploadCapability struct {
	Kind      string
	UploadID  string
	ExpiresAt time.Time
}

func lockOSSOriginalUploadKeyTx(ctx context.Context, tx pgx.Tx, cfg ossConfigPayload, key string) error {
	_, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('oss-completion:'||$1,0))`, cfg.Bucket+":"+cfg.Endpoint+":"+key)
	return err
}

// The normal tombstone transaction creates this durable marker. It never takes
// the original-key mutex while holding the file row; issuers and final adoption
// check the marker under that mutex and the deletion worker rechecks all grants.
func requireOSSOriginalUploadLiveTx(ctx context.Context, tx pgx.Tx, cfg ossConfigPayload, key string) error {
	var retired bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from oss_object_deletion_outbox where bucket=$1 and endpoint=$2 and object_key=$3 and raw_upload_binding_id is not null)`, cfg.Bucket, cfg.Endpoint, key).Scan(&retired); err != nil {
		return err
	}
	if retired {
		return errOSSRawUploadRetired
	}
	// The separately authorized seven-day maintenance retirement remains final.
	// Its policy and proof requirements are unchanged by terminal-file cleanup.
	if err := tx.QueryRow(ctx, `select exists(select 1 from oss_upload_maintenance_actions where operation='cleanup_raw' and bucket=$1 and endpoint=$2 and target_key=$3)`, cfg.Bucket, cfg.Endpoint, key).Scan(&retired); err != nil {
		return err
	}
	if retired {
		return errOSSRawUploadRetired
	}
	return nil
}

func recordOSSUploadCapabilityTx(ctx context.Context, tx pgx.Tx, cfg ossConfigPayload, key string, actor int64, grant ossUploadCapability) error {
	if actor <= 0 || key == "" || grant.ExpiresAt.IsZero() || (grant.Kind != "put" && grant.Kind != "multipart") || (grant.Kind == "multipart" && grant.UploadID == "") || (grant.Kind == "put" && grant.UploadID != "") {
		return errors.New("invalid actual OSS upload capability")
	}
	if err := lockOSSOriginalUploadKeyTx(ctx, tx, cfg, key); err != nil {
		return err
	}
	if err := requireOSSOriginalUploadLiveTx(ctx, tx, cfg, key); err != nil {
		return err
	}
	var recorded bool
	err := tx.QueryRow(ctx, `insert into oss_upload_capabilities(bucket,endpoint,object_key,last_actor_id,grant_kind,upload_id,max_expires_at)
 select $1,$2,$3,$4,$5,$6,$7 where $7::timestamptz>clock_timestamp()
 on conflict(bucket,endpoint,object_key) do update set
 last_actor_id=excluded.last_actor_id,grant_kind=excluded.grant_kind,upload_id=excluded.upload_id,
 max_expires_at=greatest(oss_upload_capabilities.max_expires_at,excluded.max_expires_at),
 last_issued_at=clock_timestamp(),grant_count=oss_upload_capabilities.grant_count+1
 returning true`, cfg.Bucket, cfg.Endpoint, key, actor, grant.Kind, grant.UploadID, grant.ExpiresAt).Scan(&recorded)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("actual OSS upload capability expired before persistence")
	}
	return err
}

// Resuming creates no new business file/session/job/reward. Its already signed
// authorization must nevertheless be durably recorded before any URL is sent.
func (s *Server) recordResumedOSSUploadCapability(r *http.Request, cfg ossConfigPayload, key string, grant ossUploadCapability) error {
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	actor := currentClaims(r).Subject
	if err := recordOSSUploadCapabilityTx(ctx, tx, cfg, key, actor, grant); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `update oss_user_upload_quota_reservations set expires_at=greatest(expires_at,$3) where user_id=$1 and object_key=$2`, actor, key, grant.ExpiresAt); err != nil {
		return err
	}
	if grant.UploadID != "" {
		if _, err := tx.Exec(ctx, `update oss_multipart_sessions set expires_at=greatest(expires_at,$6),updated_at=clock_timestamp() where owner_id=$1 and bucket=$2 and endpoint=$3 and object_key=$4 and upload_id=$5 and status='active'`, actor, cfg.Bucket, cfg.Endpoint, key, grant.UploadID, grant.ExpiresAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
