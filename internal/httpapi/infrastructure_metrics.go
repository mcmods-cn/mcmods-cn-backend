package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/queue"
)

func (s *Server) infrastructureMetrics(w http.ResponseWriter, r *http.Request) {
	durable, err := s.loadInfrastructureDurableMetrics(r.Context())
	if err != nil {
		slog.Error("failed to load durable infrastructure metrics", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load infrastructure metrics")
		return
	}
	connections, realtimeUsers := s.realtime.connectionCount()
	realtimeRejected, realtimeDropped := s.realtime.deliveryStats()
	queueStatus := s.queue.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"health":           map[string]any{"liveRequests": s.liveRequests.Load(), "readyRequests": s.readyRequests.Load(), "databasePings": s.databasePings.Load()},
		"securityVersions": map[string]any{"refreshFailures": s.securityVersionRefreshFailures.Load()},
		"redis":            s.cache.Metrics(),
		"accessLogs":       s.accessLogs.Metrics(),
		"antiAbuse":        s.antiAbuse.AsyncMetrics(),
		"queue":            map[string]any{"enabled": queueStatus.Enabled, "connected": queueStatus.Connected, "jetStream": queueStatus.JetStream},
		"outbox": map[string]any{
			"pending": durable.PendingOutbox, "oldestSeconds": durable.OldestSeconds, "deadLetters": durable.DeadLetters,
		},
		"ossDeletion":                map[string]any{"dead": durable.OSSDeletionDead},
		"ossRehome":                  map[string]any{"dead": durable.OSSRehomeDead},
		"ossMultipart":               map[string]any{"dead": durable.OSSMultipartDead},
		"ossWrites":                  s.ossWrites.snapshot(),
		"projectUpdateNotifications": projectUpdateNotificationObservability.snapshot(),
		"realtime": map[string]any{
			"connections": connections, "users": realtimeUsers, "rejected": realtimeRejected, "dropped": realtimeDropped,
			"limits": map[string]any{"total": maxRealtimeConnections, "perUser": maxRealtimeConnectionsPerUser, "perSession": maxRealtimeConnectionsPerSession, "lifetimeSeconds": int(maxRealtimeConnectionLifetime / time.Second)},
		},
		"sampledAt": time.Now().UTC(),
	})
}

type infrastructureDurableMetrics struct {
	PendingOutbox    int64
	OldestSeconds    float64
	DeadLetters      int64
	OSSDeletionDead  int64
	OSSRehomeDead    int64
	OSSMultipartDead int64
}

func (s *Server) loadInfrastructureDurableMetrics(ctx context.Context) (infrastructureDurableMetrics, error) {
	var metrics infrastructureDurableMetrics
	err := s.db.QueryRow(ctx, `select
		(select count(*) from nats_outbox where published_at is null and status in ('pending','failed','publishing')),
		coalesce((select extract(epoch from now()-min(created_at)) from nats_outbox where published_at is null and status in ('pending','failed','publishing')),0),
		(select count(*) from dead_letter_events where replayed_at is null),
		(select count(*) from oss_object_deletion_outbox where status='dead'),
		(select count(*) from oss_rehome_jobs where status='dead'),
		(select count(*) from oss_multipart_sessions where status='dead')`).
		Scan(&metrics.PendingOutbox, &metrics.OldestSeconds, &metrics.DeadLetters, &metrics.OSSDeletionDead, &metrics.OSSRehomeDead, &metrics.OSSMultipartDead)
	if err != nil {
		return infrastructureDurableMetrics{}, fmt.Errorf("query durable infrastructure metrics: %w", err)
	}
	return metrics, nil
}

func (s *Server) adminDeadLetters(w http.ResponseWriter, r *http.Request) {
	page, err := parseDeadLetterPageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid dead letter page")
		return
	}
	query, arguments := deadLetterPageSQL(page)
	rows, err := s.db.Query(r.Context(), query, arguments...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dead letters")
		return
	}
	defer rows.Close()
	items := make([]deadLetterPageItem, 0, page.Limit+1)
	for rows.Next() {
		var item deadLetterPageItem
		if err = rows.Scan(&item.ID, &item.EventID, &item.EventType, &item.Subject, &item.FailureStage,
			&item.AggregateType, &item.AggregateID, &item.Attempts, &item.LastError, &item.FailedAt, &item.ReplayedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode dead letters")
			return
		}
		if item.ReplayedAt == nil {
			item.Status = "unresolved"
		} else {
			item.Status = "replayed"
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dead letters")
		return
	}
	hasMore := len(items) > page.Limit
	if hasMore {
		items = items[:page.Limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = encodeDeadLetterPageCursor(page, last.FailedAt, last.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "limit": page.Limit, "hasMore": hasMore, "nextCursor": nextCursor,
	})
}

func deadLetterPageSQL(page deadLetterPageRequest) (string, []any) {
	query := `select id,event_id,event_type,subject,failure_stage,aggregate_type,aggregate_id,
		attempts,left(last_error,1000),failed_at,replayed_at from dead_letter_events`
	clauses := make([]string, 0, 4)
	arguments := make([]any, 0, 6)
	switch page.Status {
	case "unresolved":
		clauses = append(clauses, "replayed_at is null")
	case "replayed":
		clauses = append(clauses, "replayed_at is not null")
	}
	if page.AggregateType != "" {
		arguments = append(arguments, page.AggregateType)
		clauses = append(clauses, fmt.Sprintf("aggregate_type=$%d", len(arguments)))
	}
	if page.AggregateID != "" {
		arguments = append(arguments, page.AggregateID)
		clauses = append(clauses, fmt.Sprintf("aggregate_id=$%d", len(arguments)))
	}
	if page.AfterID > 0 {
		arguments = append(arguments, page.AfterFailedAt, page.AfterID)
		clauses = append(clauses, fmt.Sprintf("(failed_at,id)<($%d,$%d)", len(arguments)-1, len(arguments)))
	}
	if len(clauses) > 0 {
		query += " where " + strings.Join(clauses, " and ")
	}
	arguments = append(arguments, page.Limit+1)
	query += fmt.Sprintf(" order by failed_at desc,id desc limit $%d", len(arguments))
	return query, arguments
}

type deadLetterPageRequest struct {
	Status                     string
	AggregateType, AggregateID string
	Limit                      int
	AfterID                    int64
	AfterFailedAt              time.Time
}

type deadLetterPageCursor struct {
	Version  int    `json:"v"`
	FailedAt string `json:"failedAt"`
	ID       int64  `json:"id"`
	Scope    string `json:"scope"`
}

type deadLetterPageItem struct {
	ID            int64      `json:"id"`
	EventID       string     `json:"eventId"`
	EventType     string     `json:"eventType"`
	Subject       string     `json:"subject"`
	FailureStage  string     `json:"failureStage"`
	AggregateType string     `json:"aggregateType"`
	AggregateID   string     `json:"aggregateId"`
	Attempts      int64      `json:"attempts"`
	LastError     string     `json:"lastError"`
	FailedAt      time.Time  `json:"failedAt"`
	ReplayedAt    *time.Time `json:"replayedAt"`
	Status        string     `json:"status"`
}

func parseDeadLetterPageRequest(r *http.Request) (deadLetterPageRequest, error) {
	page := deadLetterPageRequest{Status: "unresolved", Limit: 50}
	values := r.URL.Query()
	allowed := map[string]bool{"status": true, "aggregateType": true, "aggregateId": true, "limit": true, "cursor": true}
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 {
			return deadLetterPageRequest{}, errors.New("unknown or repeated dead-letter page parameter")
		}
	}
	if entries, ok := values["status"]; ok {
		page.Status = entries[0]
	}
	if page.Status != "unresolved" && page.Status != "replayed" && page.Status != "all" {
		return deadLetterPageRequest{}, errors.New("invalid dead-letter status")
	}
	page.AggregateType = strings.TrimSpace(values.Get("aggregateType"))
	page.AggregateID = strings.TrimSpace(values.Get("aggregateId"))
	if page.AggregateType != values.Get("aggregateType") || page.AggregateID != values.Get("aggregateId") ||
		len(page.AggregateType) > 100 || len(page.AggregateID) > 200 || (page.AggregateID != "" && page.AggregateType == "") {
		return deadLetterPageRequest{}, errors.New("invalid dead-letter aggregate filter")
	}
	if value, ok := values["limit"]; ok {
		parsed, parseErr := strconv.Atoi(value[0])
		if parseErr != nil || parsed < 1 || parsed > 100 {
			return deadLetterPageRequest{}, errors.New("invalid dead-letter page limit")
		}
		page.Limit = parsed
	}
	scope := deadLetterPageScope(page)
	if value, ok := values["cursor"]; ok {
		cursor, parseErr := decodeDeadLetterPageCursor(value[0])
		if parseErr != nil || cursor.Scope != scope {
			return deadLetterPageRequest{}, errors.New("invalid dead-letter page cursor")
		}
		page.AfterFailedAt, parseErr = time.Parse(time.RFC3339Nano, cursor.FailedAt)
		if parseErr != nil || page.AfterFailedAt.Location() != time.UTC ||
			page.AfterFailedAt.Format(time.RFC3339Nano) != cursor.FailedAt || cursor.ID <= 0 {
			return deadLetterPageRequest{}, errors.New("invalid dead-letter page cursor position")
		}
		page.AfterID = cursor.ID
	}
	return page, nil
}

func deadLetterPageScope(page deadLetterPageRequest) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		page.Status, page.AggregateType, page.AggregateID, strconv.Itoa(page.Limit),
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func encodeDeadLetterPageCursor(page deadLetterPageRequest, failedAt time.Time, id int64) string {
	payload, _ := json.Marshal(deadLetterPageCursor{
		Version: 1, FailedAt: failedAt.UTC().Format(time.RFC3339Nano), ID: id, Scope: deadLetterPageScope(page),
	})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeDeadLetterPageCursor(value string) (deadLetterPageCursor, error) {
	if value == "" || len(value) > 2048 {
		return deadLetterPageCursor{}, errors.New("invalid dead-letter cursor length")
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return deadLetterPageCursor{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var cursor deadLetterPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return deadLetterPageCursor{}, err
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF || cursor.Version != 1 || len(cursor.Scope) != 64 {
		return deadLetterPageCursor{}, errors.New("invalid dead-letter cursor payload")
	}
	return cursor, nil
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
			values('security','warning','dead_letter_replayed',$1,jsonb_build_object(
				'actorUserId',$2::bigint,'originalEventId',$3::text,'newEventId',$4::text))`,
			strconv.FormatInt(id, 10), claims.Subject, eventID, newEventID)
	}
	if err != nil {
		slog.Error("failed to record dead letter replay", "dead_letter_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to commit dead letter replay")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		slog.Error("failed to commit dead letter replay", "dead_letter_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to commit dead letter replay")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": true, "eventId": newEventID})
}
