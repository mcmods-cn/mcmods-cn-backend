package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Server) adminOSSRehomeJobs(w http.ResponseWriter, r *http.Request) {
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status == "" {
		status = "dead"
	}
	if !stringSet("queued", "processing", "completed", "dead")[status] {
		writeError(w, http.StatusBadRequest, "invalid OSS rehome status")
		return
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 50, 100)
	rows, err := s.db.Query(r.Context(), `select public_id,(select project_code from mods where id=mod_id),status,generation,claimed_generation,attempts,max_attempts,
		failure_class,left(last_error,1000),created_at,updated_at,completed_at,dead_at,replay_count,last_replayed_at,(select public_id from users where id=last_replayed_by)
		from oss_rehome_jobs where status=$1
		order by case when status='dead' then dead_at else updated_at end desc nulls last,id desc limit $2`, status, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load OSS rehome jobs")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var id, modID string
		var generation int64
		var claimedGeneration *int64
		var lastReplayedBy *string
		var itemStatus, failureClass, lastError string
		var attempts, maxAttempts, replayCount int
		var createdAt, updatedAt time.Time
		var completedAt, deadAt, lastReplayedAt *time.Time
		if err = rows.Scan(&id, &modID, &itemStatus, &generation, &claimedGeneration, &attempts, &maxAttempts,
			&failureClass, &lastError, &createdAt, &updatedAt, &completedAt, &deadAt, &replayCount, &lastReplayedAt, &lastReplayedBy); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode OSS rehome jobs")
			return
		}
		items = append(items, map[string]any{
			"id": id, "modId": modID, "status": itemStatus, "generation": generation, "claimedGeneration": claimedGeneration,
			"attempts": attempts, "maxAttempts": maxAttempts, "failureClass": failureClass, "lastError": lastError,
			"createdAt": createdAt, "updatedAt": updatedAt, "completedAt": completedAt, "deadAt": deadAt,
			"replayCount": replayCount, "lastReplayedAt": lastReplayedAt, "lastReplayedBy": lastReplayedBy,
		})
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "failed to load OSS rehome jobs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminReplayOSSRehome(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "invalid OSS rehome job id")
		return
	}
	ctx := r.Context()
	fail := func(err error) {
		slog.Error("replay OSS rehome job", "id", publicID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to replay OSS rehome job")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		fail(err)
		return
	}
	defer tx.Rollback(ctx)
	var id int64
	var modID, generation int64
	var status, failureClass, lastError string
	var attempts, maxAttempts int
	err = tx.QueryRow(ctx, `select id,status,mod_id,generation,attempts,max_attempts,failure_class,last_error
		from oss_rehome_jobs where public_id=$1 for update`, publicID).
		Scan(&id, &status, &modID, &generation, &attempts, &maxAttempts, &failureClass, &lastError)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "OSS rehome job not found")
		return
	}
	if err != nil {
		fail(err)
		return
	}
	actor, ok := requireCurrentWritePermission(w, r, tx, "admin.config.write")
	if !ok {
		return
	}
	if status != "dead" {
		writeError(w, http.StatusConflict, "OSS rehome job is not dead")
		return
	}
	tag, err := tx.Exec(ctx, `update oss_rehome_jobs set status='queued',generation=generation+1,claimed_generation=null,
        attempts=0,next_attempt_at=now(),locked_by='',lease_expires_at=null,last_error='',failure_class='',
        completed_at=null,dead_at=null,replay_count=replay_count+1,last_replayed_at=now(),
		last_replayed_by=$2,updated_at=now() where id=$1 and status='dead'`, id, actor.Subject)
	if err != nil {
		fail(err)
		return
	}
	if tag.RowsAffected() != 1 {
		writeError(w, http.StatusConflict, "OSS rehome job is not dead")
		return
	}
	_, err = tx.Exec(ctx, `insert into app_logs(category,level,action,target,payload)
		values('security','warning','oss_rehome_replayed',$1::text,jsonb_build_object(
			'actorUserId',$2::bigint,'modId',$3::bigint,'generation',$4::bigint,'attempts',$5::integer,
        'maxAttempts',$6::integer,'failureClass',$7::text,'lastError',$8::text))`,
		strconv.FormatInt(id, 10), actor.Subject, modID, generation, attempts, maxAttempts, failureClass, lastError)
	if err == nil {
		err = enqueueRequestActivityTx(r, tx)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		fail(err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": true, "id": publicID})
}
