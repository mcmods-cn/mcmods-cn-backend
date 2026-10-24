package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var errOSSMultipartNotReplayable = errors.New("OSS multipart session is not dead")

func (s *Server) adminOSSMultipartSessions(w http.ResponseWriter, r *http.Request) {
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status == "" {
		status = "dead"
	}
	if !stringSet("active", "completing", "cleanup_pending", "aborting", "completed", "aborted", "dead")[status] {
		writeError(w, http.StatusBadRequest, "invalid OSS multipart status")
		return
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 50, 100)
	rows, err := s.db.Query(r.Context(), `select public_id,(select public_id from users where id=owner_id),bucket,object_key,upload_id,original_name,size_bytes,status,
		attempts,max_attempts,failure_class,left(last_error,1000),expires_at,created_at,updated_at,completed_at,aborted_at,dead_at,
		replay_count,last_replayed_at,(select public_id from users where id=last_replayed_by) from oss_multipart_sessions where status=$1
		order by case when status='dead' then dead_at else updated_at end desc nulls last,id desc limit $2`, status, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load OSS multipart sessions")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var id string
		var sizeBytes int64
		var ownerID, replayedBy *string
		var bucket, objectKey, uploadID, originalName, itemStatus, failureClass, lastError string
		var attempts, maxAttempts, replayCount int
		var expiresAt, createdAt, updatedAt time.Time
		var completedAt, abortedAt, deadAt, replayedAt *time.Time
		if err = rows.Scan(&id, &ownerID, &bucket, &objectKey, &uploadID, &originalName, &sizeBytes, &itemStatus,
			&attempts, &maxAttempts, &failureClass, &lastError, &expiresAt, &createdAt, &updatedAt, &completedAt, &abortedAt, &deadAt,
			&replayCount, &replayedAt, &replayedBy); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode OSS multipart sessions")
			return
		}
		items = append(items, map[string]any{"id": id, "ownerId": ownerID, "bucket": bucket, "objectKey": objectKey,
			"uploadId": uploadID, "originalName": originalName, "sizeBytes": sizeBytes, "status": itemStatus,
			"attempts": attempts, "maxAttempts": maxAttempts, "failureClass": failureClass, "lastError": lastError,
			"expiresAt": expiresAt, "createdAt": createdAt, "updatedAt": updatedAt, "completedAt": completedAt,
			"abortedAt": abortedAt, "deadAt": deadAt, "replayCount": replayCount, "lastReplayedAt": replayedAt, "lastReplayedBy": replayedBy})
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "failed to load OSS multipart sessions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminReplayOSSMultipartSession(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "invalid OSS multipart session id")
		return
	}
	var id int64
	err := s.db.QueryRow(r.Context(), `select id from oss_multipart_sessions where public_id=$1`, publicID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "OSS job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve OSS job")
		return
	}

	err = s.replayOSSMultipartSession(r.Context(), id, currentClaims(r).Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "OSS multipart session not found")
		return
	}
	if errors.Is(err, errOSSMultipartNotReplayable) {
		writeError(w, http.StatusConflict, "OSS multipart session is not dead")
		return
	}
	if err != nil {
		slog.Error("replay OSS multipart cleanup", "session_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to replay OSS multipart cleanup")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": true, "id": publicID})
}

func (s *Server) replayOSSMultipartSession(ctx context.Context, id, actorID int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status, failureClass, lastError string
	var attempts, maxAttempts int
	if err = tx.QueryRow(ctx, `select status,attempts,max_attempts,failure_class,last_error
		from oss_multipart_sessions where id=$1 for update`, id).Scan(&status, &attempts, &maxAttempts, &failureClass, &lastError); err != nil {
		return err
	}
	if status != "dead" {
		return errOSSMultipartNotReplayable
	}
	if _, err = tx.Exec(ctx, `update oss_multipart_sessions set status='cleanup_pending',attempts=0,next_attempt_at=now(),
		locked_by='',lease_expires_at=null,last_error='',failure_class='',dead_at=null,replay_count=replay_count+1,
		last_replayed_at=now(),last_replayed_by=nullif($2,0),updated_at=now() where id=$1 and status='dead'`, id, actorID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into app_logs(category,level,action,target,payload)
		values('security','warning','oss_multipart_cleanup_replayed',$1::text,jsonb_build_object(
			'actorUserId',$2::bigint,'attempts',$3::integer,'maxAttempts',$4::integer,
			'failureClass',$5::text,'lastError',$6::text))`, strconv.FormatInt(id, 10), actorID, attempts, maxAttempts, failureClass, lastError); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
