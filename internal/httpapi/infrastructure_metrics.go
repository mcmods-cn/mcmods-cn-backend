package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/queue"
)

func (s *Server) infrastructureMetrics(w http.ResponseWriter, r *http.Request) {
	connections, realtimeUsers := s.realtime.connectionCount()
	queueStatus := s.queue.Status()
	var pendingOutbox, deadLetters int64
	var oldestSeconds float64
	if err := s.db.QueryRow(r.Context(), `select
		(select count(*) from nats_outbox where published_at is null and status in ('pending','failed','publishing')),
		coalesce((select extract(epoch from now()-min(created_at)) from nats_outbox where published_at is null and status in ('pending','failed','publishing')),0),
		(select count(*) from dead_letter_events where replayed_at is null)`).Scan(&pendingOutbox, &oldestSeconds, &deadLetters); err != nil {
		writeError(w, http.StatusServiceUnavailable, "infrastructure metrics are temporarily unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"health":    map[string]any{"liveRequests": s.liveRequests.Load(), "readyRequests": s.readyRequests.Load(), "databasePings": s.databasePings.Load()},
		"redis":     s.cache.Metrics(),
		"queue":     map[string]any{"enabled": queueStatus.Enabled, "connected": queueStatus.Connected, "jetStream": queueStatus.JetStream},
		"outbox":    map[string]any{"pending": pendingOutbox, "oldestSeconds": oldestSeconds, "deadLetters": deadLetters},
		"realtime":  map[string]any{"connections": connections, "users": realtimeUsers},
		"sampledAt": time.Now().UTC(),
	})
}

func (s *Server) adminDeadLetters(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if parsed, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && parsed > 0 && parsed <= 100 {
		limit = parsed
	}
	rows, err := s.db.Query(r.Context(), `select id,event_id,event_type,subject,failure_stage,attempts,left(last_error,1000),failed_at,replayed_at
		from dead_letter_events order by failed_at desc,id desc limit $1`, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dead letters")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var id, attempts int64
		var eventID, eventType, subject, stage, lastError string
		var failedAt time.Time
		var replayedAt *time.Time
		if err = rows.Scan(&id, &eventID, &eventType, &subject, &stage, &attempts, &lastError, &failedAt, &replayedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode dead letters")
			return
		}
		items = append(items, map[string]any{"id": id, "eventId": eventID, "eventType": eventType, "subject": subject,
			"failureStage": stage, "attempts": attempts, "lastError": lastError, "failedAt": failedAt, "replayedAt": replayedAt})
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dead letters")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminReplayDeadLetter(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid dead letter id")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin dead letter replay")
		return
	}
	defer tx.Rollback(r.Context())
	var eventID, eventType, subject, stage string
	var payload []byte
	var replayedAt *time.Time
	err = tx.QueryRow(r.Context(), `select event_id,event_type,subject,payload,failure_stage,replayed_at
		from dead_letter_events where id=$1 for update`, id).Scan(&eventID, &eventType, &subject, &payload, &stage, &replayedAt)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "dead letter not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dead letter")
		return
	}
	if replayedAt != nil {
		writeError(w, http.StatusConflict, "dead letter was already replayed")
		return
	}
	newEventID := eventID
	if stage == "publish" {
		tag, updateErr := tx.Exec(r.Context(), `update nats_outbox set status='pending',published_at=null,attempts=0,last_error='',
			available_at=now(),locked_at=null,locked_by='',updated_at=now() where event_id=$1`, eventID)
		if updateErr != nil || tag.RowsAffected() != 1 {
			writeError(w, http.StatusConflict, "original outbox event cannot be replayed")
			return
		}
	} else {
		var envelope queue.EventEnvelope
		if json.Unmarshal(payload, &envelope) != nil || envelope.EventID == "" || envelope.SchemaVersion != 1 || envelope.AggregateType == "" || envelope.AggregateID == "" {
			writeError(w, http.StatusConflict, "dead letter event schema is unsupported")
			return
		}
		newEventID, err = queue.EnqueueTx(r.Context(), tx, subject, eventType, envelope.AggregateType, envelope.AggregateID,
			envelope.TraceID, json.RawMessage(envelope.Payload))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to enqueue dead letter replay")
			return
		}
	}
	claims := currentClaims(r)
	if _, err = tx.Exec(r.Context(), `update dead_letter_events set replayed_at=now() where id=$1`, id); err == nil {
		_, err = tx.Exec(r.Context(), `insert into app_logs(category,level,action,target,payload)
			values('security','warning','dead_letter_replayed',$1,jsonb_build_object('actorUserId',$2,'originalEventId',$3,'newEventId',$4))`,
			strconv.FormatInt(id, 10), claims.Subject, eventID, newEventID)
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit dead letter replay")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": true, "eventId": newEventID})
}
