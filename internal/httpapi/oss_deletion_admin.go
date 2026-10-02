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

var errOSSDeletionNotReplayable = errors.New("OSS deletion job is not dead")

func (s *Server) adminOSSDeletionJobs(w http.ResponseWriter, r *http.Request) {
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status == "" {
		status = "dead"
	}
	if !stringSet("pending", "processing", "completed", "dead")[status] {
		writeError(w, http.StatusBadRequest, "invalid OSS deletion status")
		return
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 50, 100)
	rows, err := s.db.Query(r.Context(), `select id,oss_file_id,bucket,object_key,reason,status,attempts,max_attempts,
		failure_class,left(last_error,1000),created_at,updated_at,deleted_at,dead_at,replay_count,last_replayed_at,last_replayed_by
		from oss_object_deletion_outbox where status=$1
		order by case when status='dead' then dead_at else updated_at end desc nulls last,id desc limit $2`, status, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load OSS deletion jobs")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var id int64
		var fileID, lastReplayedBy *int64
		var bucket, objectKey, reason, itemStatus, failureClass, lastError string
		var attempts, maxAttempts, replayCount int
		var createdAt, updatedAt time.Time
		var deletedAt, deadAt, lastReplayedAt *time.Time
		if err = rows.Scan(&id, &fileID, &bucket, &objectKey, &reason, &itemStatus, &attempts, &maxAttempts,
			&failureClass, &lastError, &createdAt, &updatedAt, &deletedAt, &deadAt, &replayCount, &lastReplayedAt, &lastReplayedBy); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode OSS deletion jobs")
			return
		}
		items = append(items, map[string]any{
			"id": id, "fileId": fileID, "bucket": bucket, "objectKey": objectKey, "reason": reason, "status": itemStatus,
			"attempts": attempts, "maxAttempts": maxAttempts, "failureClass": failureClass, "lastError": lastError,
			"createdAt": createdAt, "updatedAt": updatedAt, "deletedAt": deletedAt, "deadAt": deadAt,
			"replayCount": replayCount, "lastReplayedAt": lastReplayedAt, "lastReplayedBy": lastReplayedBy,
		})
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "failed to load OSS deletion jobs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminReplayOSSDeletion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid OSS deletion job id")
		return
	}
	err = s.replayOSSDeletion(r.Context(), id, currentClaims(r).Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "OSS deletion job not found")
		return
	}
	if errors.Is(err, errOSSDeletionNotReplayable) {
		writeError(w, http.StatusConflict, "OSS deletion job is not dead")
		return
	}
	if err != nil {
		slog.Error("replay OSS deletion job", "job_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to replay OSS deletion job")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": true, "id": id})
}

func (s *Server) replayOSSDeletion(ctx context.Context, id, actorID int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status, failureClass, lastError string
	var attempts, maxAttempts int
	if err = tx.QueryRow(ctx, `select status,attempts,max_attempts,failure_class,last_error
		from oss_object_deletion_outbox where id=$1 for update`, id).
		Scan(&status, &attempts, &maxAttempts, &failureClass, &lastError); err != nil {
		return err
	}
	if status != "dead" {
		return errOSSDeletionNotReplayable
	}
	tag, err := tx.Exec(ctx, `update oss_object_deletion_outbox set status='pending',attempts=0,next_attempt_at=now(),
		locked_at=null,locked_by='',last_error='',failure_class='',dead_at=null,deleted_at=null,
		replay_count=replay_count+1,last_replayed_at=now(),last_replayed_by=nullif($2,0),updated_at=now()
		where id=$1 and status='dead'`, id, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errOSSDeletionNotReplayable
	}
	_, err = tx.Exec(ctx, `insert into app_logs(category,level,action,target,payload)
		values('security','warning','oss_deletion_replayed',$1::text,jsonb_build_object(
			'actorUserId',$2::bigint,'attempts',$3::integer,'maxAttempts',$4::integer,
			'failureClass',$5::text,'lastError',$6::text))`,
		strconv.FormatInt(id, 10), actorID, attempts, maxAttempts, failureClass, lastError)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
