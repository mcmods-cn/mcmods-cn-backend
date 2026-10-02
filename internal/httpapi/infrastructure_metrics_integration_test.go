package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

func TestAdminDeadLetterReplayRestoresPublishAndConsumerFailuresIntegration(t *testing.T) {
	pool := openDeadLetterReplayTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	server := &Server{db: pool}

	var publishDeadLetterID int64
	if _, err := pool.Exec(ctx, `insert into nats_outbox(
			event_id,event_type,subject,aggregate_type,aggregate_id,payload,status,published_at,locked_at,locked_by,attempts,last_error)
		values('publish-event','test.publish','publish_task','test','publish','{}','dead',now(),now(),'old-worker',12,'publish failed')`); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into dead_letter_events(event_id,event_type,subject,payload,failure_stage,attempts,last_error)
		values('publish-event','test.publish','publish_task','{}','publish',12,'publish failed') returning id`).Scan(&publishDeadLetterID); err != nil {
		t.Fatal(err)
	}
	publishResponse := replayDeadLetter(t, server, publishDeadLetterID, 1001)
	if publishResponse.Code != http.StatusAccepted {
		t.Fatalf("publish replay status = %d, body = %s", publishResponse.Code, publishResponse.Body.String())
	}
	var publishPayload struct {
		Data struct {
			Queued  bool   `json:"queued"`
			EventID string `json:"eventId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(publishResponse.Body.Bytes(), &publishPayload); err != nil {
		t.Fatal(err)
	}
	if !publishPayload.Data.Queued || publishPayload.Data.EventID != "publish-event" {
		t.Fatalf("unexpected publish replay response: %#v", publishPayload)
	}
	var publishStatus, publishLockedBy, publishLastError string
	var publishAttempts int64
	var publishPublishedAt, publishLockedAt *time.Time
	if err := pool.QueryRow(ctx, `select status,published_at,locked_at,locked_by,attempts,last_error
		from nats_outbox where event_id='publish-event'`).Scan(
		&publishStatus, &publishPublishedAt, &publishLockedAt, &publishLockedBy, &publishAttempts, &publishLastError); err != nil {
		t.Fatal(err)
	}
	if publishStatus != "pending" || publishPublishedAt != nil || publishLockedAt != nil || publishLockedBy != "" || publishAttempts != 0 || publishLastError != "" {
		t.Fatalf("publish replay did not reset the authoritative outbox row: status=%q published=%v locked=%v/%q attempts=%d error=%q",
			publishStatus, publishPublishedAt, publishLockedAt, publishLockedBy, publishAttempts, publishLastError)
	}
	assertDeadLetterReplayAudit(t, ctx, pool, publishDeadLetterID, 1001, "publish-event", "publish-event")
	conflictResponse := replayDeadLetter(t, server, publishDeadLetterID, 1001)
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("second replay status = %d, want conflict; body = %s", conflictResponse.Code, conflictResponse.Body.String())
	}

	originalEnvelope := queue.EventEnvelope{
		EventID: "consumer-event", EventType: "test.consume", SchemaVersion: 1,
		OccurredAt: time.Now().UTC(), AggregateType: "test", AggregateID: "consumer",
		TraceID: "trace-consumer", Payload: json.RawMessage(`{"recover":true}`),
	}
	originalRaw, err := json.Marshal(originalEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	var consumerDeadLetterID int64
	if err = pool.QueryRow(ctx, `insert into dead_letter_events(event_id,event_type,subject,payload,failure_stage,attempts,last_error)
		values($1,$2,'consumer_task',$3::jsonb,'consumer:consumer_task',3,'consumer failed') returning id`,
		originalEnvelope.EventID, originalEnvelope.EventType, string(originalRaw)).Scan(&consumerDeadLetterID); err != nil {
		t.Fatal(err)
	}
	consumerResponse := replayDeadLetter(t, server, consumerDeadLetterID, 1001)
	if consumerResponse.Code != http.StatusAccepted {
		t.Fatalf("consumer replay status = %d, body = %s", consumerResponse.Code, consumerResponse.Body.String())
	}
	var consumerPayload struct {
		Data struct {
			Queued  bool   `json:"queued"`
			EventID string `json:"eventId"`
		} `json:"data"`
	}
	if err = json.Unmarshal(consumerResponse.Body.Bytes(), &consumerPayload); err != nil {
		t.Fatal(err)
	}
	if !consumerPayload.Data.Queued || consumerPayload.Data.EventID == "" || consumerPayload.Data.EventID == originalEnvelope.EventID {
		t.Fatalf("consumer replay must enqueue a fresh event identity: %#v", consumerPayload)
	}
	var eventType, subject, aggregateType, aggregateID, traceID, status string
	var payload []byte
	if err = pool.QueryRow(ctx, `select event_type,subject,aggregate_type,aggregate_id,trace_id,payload,status
		from nats_outbox where event_id=$1`, consumerPayload.Data.EventID).Scan(
		&eventType, &subject, &aggregateType, &aggregateID, &traceID, &payload, &status); err != nil {
		t.Fatal(err)
	}
	var replayedData map[string]bool
	if err = json.Unmarshal(payload, &replayedData); err != nil {
		t.Fatal(err)
	}
	if eventType != originalEnvelope.EventType || subject != "consumer_task" || aggregateType != originalEnvelope.AggregateType ||
		aggregateID != originalEnvelope.AggregateID || traceID != originalEnvelope.TraceID || !replayedData["recover"] || len(replayedData) != 1 || status != "pending" {
		t.Fatalf("consumer replay outbox row lost envelope identity: event=%q subject=%q aggregate=%q/%q trace=%q payload=%s status=%q",
			eventType, subject, aggregateType, aggregateID, traceID, payload, status)
	}
	assertDeadLetterReplayAudit(t, ctx, pool, consumerDeadLetterID, 1001, originalEnvelope.EventID, consumerPayload.Data.EventID)
}

func replayDeadLetter(t *testing.T, server *Server, id, actor int64) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/infrastructure/dead-letters/replay", nil)
	request.SetPathValue("id", strconv.FormatInt(id, 10))
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: actor}))
	response := httptest.NewRecorder()
	server.adminReplayDeadLetter(response, request)
	return response
}

func assertDeadLetterReplayAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, actor int64, originalEventID, newEventID string) {
	t.Helper()
	var replayedAt *time.Time
	if err := pool.QueryRow(ctx, `select replayed_at from dead_letter_events where id=$1`, id).Scan(&replayedAt); err != nil || replayedAt == nil {
		t.Fatalf("dead letter replay timestamp = %v, error = %v", replayedAt, err)
	}
	var payload []byte
	if err := pool.QueryRow(ctx, `select payload from app_logs where action='dead_letter_replayed' and target=$1`, strconv.FormatInt(id, 10)).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var audit struct {
		ActorUserID     int64  `json:"actorUserId"`
		OriginalEventID string `json:"originalEventId"`
		NewEventID      string `json:"newEventId"`
	}
	if err := json.Unmarshal(payload, &audit); err != nil {
		t.Fatal(err)
	}
	if audit.ActorUserID != actor || audit.OriginalEventID != originalEventID || audit.NewEventID != newEventID {
		t.Fatalf("unexpected replay audit: %#v", audit)
	}
}

func openDeadLetterReplayTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify dead-letter recovery")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err = pool.Exec(ctx, `create temp table nats_outbox(
		id bigserial primary key,event_id text not null unique,event_type text not null,schema_version integer not null default 1,
		subject text not null,aggregate_type text not null,aggregate_id text not null,trace_id text not null default '',payload jsonb not null,
		occurred_at timestamptz not null default now(),status text not null default 'pending',available_at timestamptz not null default now(),
		published_at timestamptz,locked_at timestamptz,locked_by text not null default '',attempts bigint not null default 0,
		max_attempts bigint not null default 12,last_error text not null default '',updated_at timestamptz not null default now());
		create temp table dead_letter_events(
		id bigserial primary key,event_id text not null,event_type text not null,subject text not null,payload jsonb not null,
		failure_stage text not null,aggregate_type text not null default '',aggregate_id text not null default '',
		attempts bigint not null,last_error text not null,failed_at timestamptz not null default now(),
		replayed_at timestamptz,unique(event_id,failure_stage));
		create temp table app_logs(
		id bigserial primary key,category text not null,level text not null,action text not null,target text not null,payload jsonb not null)`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	return pool
}
