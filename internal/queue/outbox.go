package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OutboxMetrics struct {
	Published uint64
	Failed    uint64
	Retried   uint64
	Dead      uint64
}

type OutboxDispatcher struct {
	db        *pgxpool.Pool
	queue     *Client
	enabled   bool
	workerID  string
	published atomic.Uint64
	failed    atomic.Uint64
	retried   atomic.Uint64
	dead      atomic.Uint64
}

type outboxRecord struct {
	ID, Attempts, MaxAttempts int64
	Event                     EventEnvelope
	TaskCode                  string
}

func EnqueueTx(ctx context.Context, tx pgx.Tx, taskCode, eventType, aggregateType, aggregateID, traceID string, payload any) (string, error) {
	if tx == nil {
		return "", errors.New("outbox transaction is required")
	}
	if taskCode == "" || eventType == "" || aggregateType == "" || aggregateID == "" {
		return "", errors.New("outbox event identity is incomplete")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	eventID := randomEventID()
	_, err = tx.Exec(ctx, `insert into nats_outbox(
		event_id,event_type,schema_version,subject,aggregate_type,aggregate_id,trace_id,payload,occurred_at,status,available_at)
		values($1,$2,1,$3,$4,$5,$6,$7::jsonb,now(),'pending',now())`,
		eventID, eventType, taskCode, aggregateType, aggregateID, traceID, string(raw))
	return eventID, err
}

func NewOutboxDispatcher(db *pgxpool.Pool, client *Client, enabled bool) *OutboxDispatcher {
	return &OutboxDispatcher{db: db, queue: client, enabled: enabled, workerID: "dispatcher-" + randomEventID()}
}

func NewPostgresDeadLetterSink(db *pgxpool.Pool) DeadLetterSink {
	return func(ctx context.Context, dead DeadLetter) error {
		if db == nil {
			return errors.New("dead letter database is required")
		}
		lastError := dead.LastError
		if len(lastError) > 1000 {
			lastError = lastError[:1000]
		}
		_, err := db.Exec(ctx, `insert into dead_letter_events(
			event_id,event_type,subject,payload,failure_stage,aggregate_type,aggregate_id,attempts,last_error)
			values($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9)
			on conflict(event_id,failure_stage) do update set
			aggregate_type=excluded.aggregate_type,aggregate_id=excluded.aggregate_id,
			attempts=excluded.attempts,last_error=excluded.last_error,failed_at=now(),replayed_at=null`,
			dead.EventID, dead.EventType, dead.TaskCode, string(dead.Payload), dead.FailureStage,
			dead.AggregateType, dead.AggregateID, dead.Attempts, lastError)
		return err
	}
}

func (dispatcher *OutboxDispatcher) Start(ctx context.Context) {
	if dispatcher == nil || !dispatcher.enabled || dispatcher.db == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := dispatcher.DispatchBatch(ctx, 64); err != nil {
					slog.Error("outbox dispatch failed", "module", "outbox", "error", err)
				}
			}
		}
	}()
}

func (dispatcher *OutboxDispatcher) Metrics() OutboxMetrics {
	if dispatcher == nil {
		return OutboxMetrics{}
	}
	return OutboxMetrics{Published: dispatcher.published.Load(), Failed: dispatcher.failed.Load(), Retried: dispatcher.retried.Load(), Dead: dispatcher.dead.Load()}
}

func (dispatcher *OutboxDispatcher) DispatchBatch(ctx context.Context, limit int) (int, error) {
	if dispatcher == nil || dispatcher.db == nil || dispatcher.queue == nil {
		return 0, ErrUnavailable
	}
	if limit < 1 || limit > 500 {
		limit = 64
	}
	rows, err := dispatcher.claim(ctx, limit)
	if err != nil {
		return 0, err
	}
	for _, record := range rows {
		raw, marshalErr := json.Marshal(record.Event)
		if marshalErr != nil {
			if failErr := dispatcher.fail(ctx, record, marshalErr); failErr != nil {
				return len(rows), fmt.Errorf("persist outbox marshal failure for row %d: %w", record.ID, failErr)
			}
			continue
		}
		status := dispatcher.queue.Status()
		var publishErr error
		if status.JetStream {
			publishErr = dispatcher.queue.PublishEvent(ctx, record.TaskCode, record.Event.EventID, raw)
		} else {
			publishErr = dispatcher.queue.HandleLocally(ctx, record.TaskCode, record.Event.EventID, raw)
		}
		if publishErr != nil {
			if failErr := dispatcher.fail(ctx, record, publishErr); failErr != nil {
				return len(rows), fmt.Errorf("persist outbox publish failure for row %d: %w", record.ID, failErr)
			}
			continue
		}
		result, updateErr := dispatcher.db.Exec(ctx, `update nats_outbox set status='published',published_at=now(),locked_at=null,locked_by='',updated_at=now()
			where id=$1 and status='publishing' and locked_by=$2`, record.ID, dispatcher.workerID)
		if updateErr != nil {
			return len(rows), updateErr
		}
		if result.RowsAffected() != 1 {
			return len(rows), fmt.Errorf("outbox row %d lost publishing ownership before completion", record.ID)
		}
		dispatcher.published.Add(1)
	}
	return len(rows), nil
}

func (dispatcher *OutboxDispatcher) claim(ctx context.Context, limit int) ([]outboxRecord, error) {
	tx, err := dispatcher.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `update nats_outbox set status='failed',locked_at=null,locked_by='',available_at=now(),updated_at=now()
		where status='publishing' and locked_at<now()-interval '5 minutes'`); err != nil {
		return nil, fmt.Errorf("recover stale outbox publishing leases: %w", err)
	}
	rows, err := tx.Query(ctx, `select id,event_id,event_type,schema_version,occurred_at,aggregate_type,aggregate_id,trace_id,payload,subject,attempts,max_attempts
		from nats_outbox where published_at is null and status in ('pending','failed') and available_at<=now()
		order by available_at,id for update skip locked limit $1`, limit)
	if err != nil {
		return nil, err
	}
	result := make([]outboxRecord, 0, limit)
	for rows.Next() {
		var record outboxRecord
		if err = rows.Scan(&record.ID, &record.Event.EventID, &record.Event.EventType, &record.Event.SchemaVersion, &record.Event.OccurredAt,
			&record.Event.AggregateType, &record.Event.AggregateID, &record.Event.TraceID, &record.Event.Payload, &record.TaskCode, &record.Attempts, &record.MaxAttempts); err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, record := range result {
		command, updateErr := tx.Exec(ctx, `update nats_outbox set status='publishing',locked_at=now(),locked_by=$2,attempts=attempts+1,updated_at=now()
			where id=$1 and published_at is null and status in ('pending','failed')`, record.ID, dispatcher.workerID)
		if updateErr != nil {
			return nil, updateErr
		}
		if command.RowsAffected() != 1 {
			return nil, fmt.Errorf("outbox row %d lost claim ownership", record.ID)
		}
	}
	return result, tx.Commit(ctx)
}

func (dispatcher *OutboxDispatcher) fail(ctx context.Context, record outboxRecord, cause error) error {
	attempts := record.Attempts + 1
	errorText := cause.Error()
	if len(errorText) > 1000 {
		errorText = errorText[:1000]
	}
	if attempts >= record.MaxAttempts {
		result, err := dispatcher.db.Exec(ctx, `with moved as (
			update nats_outbox set status='dead',last_error=$2,locked_at=null,locked_by='',updated_at=now()
			where id=$1 and status='publishing' and locked_by=$3
			returning event_id,event_type,subject,payload,aggregate_type,aggregate_id,attempts
		) insert into dead_letter_events(event_id,event_type,subject,payload,failure_stage,aggregate_type,aggregate_id,attempts,last_error)
		select event_id,event_type,subject,payload,'publish',aggregate_type,aggregate_id,attempts,$2 from moved
		on conflict(event_id,failure_stage) do update set
		aggregate_type=excluded.aggregate_type,aggregate_id=excluded.aggregate_id,
		attempts=excluded.attempts,last_error=excluded.last_error,failed_at=now(),replayed_at=null`, record.ID, errorText, dispatcher.workerID)
		if err != nil {
			return fmt.Errorf("mark outbox row %d dead: %w", record.ID, err)
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("outbox row %d lost publishing ownership before dead-lettering", record.ID)
		}
		dispatcher.failed.Add(1)
		dispatcher.dead.Add(1)
		return nil
	}
	backoff := time.Second * time.Duration(1<<min(attempts, int64(8)))
	result, err := dispatcher.db.Exec(ctx, `update nats_outbox set status='failed',last_error=$2,available_at=now()+$3::interval,
		locked_at=null,locked_by='',updated_at=now() where id=$1 and status='publishing' and locked_by=$4`, record.ID, errorText, fmt.Sprintf("%f seconds", backoff.Seconds()), dispatcher.workerID)
	if err != nil {
		return fmt.Errorf("mark outbox row %d failed: %w", record.ID, err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("outbox row %d lost publishing ownership before retry", record.ID)
	}
	dispatcher.failed.Add(1)
	dispatcher.retried.Add(1)
	return nil
}
