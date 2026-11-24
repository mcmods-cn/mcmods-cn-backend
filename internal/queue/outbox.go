package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"mcmods-cn-backend/internal/diagnostic"
)

var errStreamBackpressure = errors.New("JetStream capacity unavailable")

type OutboxMetrics struct {
	Published uint64
	Failed    uint64
	Retried   uint64
	Dead      uint64
}

type OutboxDispatcher struct {
	db    *pgxpool.Pool
	queue *Client

	workerID    string
	lease       time.Duration
	mu          sync.Mutex
	active      map[string]int
	totalActive int
	published   atomic.Uint64
	failed      atomic.Uint64
	retried     atomic.Uint64
	dead        atomic.Uint64
}

type outboxRecord struct {
	ID, Attempts, MaxAttempts int64
	Event                     EventEnvelope
	TaskCode                  string
	localConsumer             bool
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

func NewOutboxDispatcher(db *pgxpool.Pool, client *Client) *OutboxDispatcher {
	return &OutboxDispatcher{db: db, queue: client, workerID: "dispatcher-" + randomEventID(), lease: 5 * time.Minute, active: make(map[string]int)}
}

func NewPostgresDeadLetterSink(db *pgxpool.Pool) DeadLetterSink {
	return func(ctx context.Context, dead DeadLetter) error {
		if db == nil {
			return errors.New("dead letter database is required")
		}
		lastError := diagnostic.Text(dead.LastError, 1000)
		payload := dead.Payload
		if !json.Valid(payload) {
			// Keep malformed input inspectable without making the jsonb sink
			// permanently fail. It remains ineligible for administrator replay.
			payload, _ = json.Marshal(map[string]string{"invalidEnvelope": string(payload)})
		}
		_, err := db.Exec(ctx, `insert into dead_letter_events(
			event_id,event_type,subject,payload,failure_stage,aggregate_type,aggregate_id,attempts,last_error)
			values($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9)
			on conflict(event_id,failure_stage) do nothing`,
			dead.EventID, dead.EventType, dead.TaskCode, string(payload), dead.FailureStage,
			dead.AggregateType, dead.AggregateID, dead.Attempts, lastError)
		return err
	}
}

func (dispatcher *OutboxDispatcher) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	if dispatcher == nil || dispatcher.db == nil {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		var work sync.WaitGroup
		defer work.Wait()
		launch := func(deliver func() error) {
			work.Go(func() {
				if err := deliver(); err != nil && ctx.Err() == nil {
					slog.Error("local outbox delivery failed", "module", "outbox", "error", err)
				}
			})
		}
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := dispatcher.dispatchAvailable(ctx, 64, launch); err != nil && ctx.Err() == nil {
					slog.Error("outbox dispatch failed", "module", "outbox", "error", err)
				}
			}
		}
	}()
	return done
}

func (dispatcher *OutboxDispatcher) Metrics() OutboxMetrics {
	if dispatcher == nil {
		return OutboxMetrics{}
	}
	return OutboxMetrics{Published: dispatcher.published.Load(), Failed: dispatcher.failed.Load(), Retried: dispatcher.retried.Load(), Dead: dispatcher.dead.Load()}
}

func (dispatcher *OutboxDispatcher) DispatchBatch(ctx context.Context, limit int) (int, error) {
	var local errgroup.Group
	count, err := dispatcher.dispatchAvailable(ctx, limit, local.Go)
	return count, errors.Join(err, local.Wait())
}

// Claim only work that can start now. A long local task keeps its own slot
// between polling ticks; other task types can still be delivered.
func (dispatcher *OutboxDispatcher) dispatchAvailable(ctx context.Context, limit int, launch func(func() error)) (int, error) {
	if dispatcher == nil || dispatcher.db == nil || dispatcher.queue == nil {
		return 0, ErrUnavailable
	}
	if limit < 1 || limit > 500 {
		limit = 64
	}
	if err := dispatcher.recoverStale(ctx, limit); err != nil {
		return 0, err
	}
	for count := 0; count < limit; count++ {
		record, local, err := dispatcher.claimNext(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return count, nil
		}
		if err != nil {
			return count, err
		}
		if local {
			launch(func() error {
				defer func() {
					dispatcher.mu.Lock()
					dispatcher.active[record.TaskCode]--
					dispatcher.totalActive--
					if dispatcher.active[record.TaskCode] == 0 {
						delete(dispatcher.active, record.TaskCode)
					}
					dispatcher.mu.Unlock()
				}()
				return dispatcher.deliver(ctx, record, true)
			})
		} else if err := dispatcher.deliver(ctx, record, false); err != nil {
			if errors.Is(err, errStreamBackpressure) {
				return count + 1, nil // stop the pass after one confirmed capacity refusal
			}
			return count + 1, err
		}
	}
	return limit, nil
}

func (dispatcher *OutboxDispatcher) recoverStale(ctx context.Context, limit int) error {
	_, err := dispatcher.db.Exec(ctx, `update nats_outbox set status='failed',locked_at=null,locked_by='',available_at=now(),updated_at=now()
		where id in (select id from nats_outbox where status='publishing' and locked_at < now()-$1::interval
		order by locked_at,id limit $2 for update skip locked)`, dispatcher.lease.String(), limit)
	if err != nil {
		return fmt.Errorf("recover stale outbox publishing leases: %w", err)
	}
	return nil
}

func (dispatcher *OutboxDispatcher) claimNext(ctx context.Context) (outboxRecord, bool, error) {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	dispatcher.queue.mu.RLock()
	tasks, local := dispatcher.queue.cfg.Tasks, dispatcher.queue.jetStream == nil
	dispatcher.queue.mu.RUnlock()
	if local && dispatcher.totalActive >= 64 {
		return outboxRecord{}, local, pgx.ErrNoRows
	}
	excluded := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if !task.Enabled || (local && dispatcher.active[task.Code] >= task.MaxConcurrent) {
			excluded = append(excluded, task.Code)
		}
	}
	var record outboxRecord
	err := dispatcher.db.QueryRow(ctx, `with candidate as (
		select id from nats_outbox where published_at is null and status in ('pending','failed') and available_at<=now()
		  and subject <> all($2::text[]) order by available_at,id limit 1 for update skip locked
	) update nats_outbox o set status='publishing',locked_at=now(),locked_by=$1,attempts=o.attempts+1,updated_at=now()
	from candidate where o.id=candidate.id
	returning o.id,o.event_id,o.event_type,o.schema_version,o.occurred_at,o.aggregate_type,o.aggregate_id,o.trace_id,
	          o.payload,o.subject,o.attempts-1,o.max_attempts`, dispatcher.workerID, excluded).
		Scan(&record.ID, &record.Event.EventID, &record.Event.EventType, &record.Event.SchemaVersion, &record.Event.OccurredAt,
			&record.Event.AggregateType, &record.Event.AggregateID, &record.Event.TraceID, &record.Event.Payload, &record.TaskCode,
			&record.Attempts, &record.MaxAttempts)
	if err == nil && local {
		dispatcher.active[record.TaskCode]++
		dispatcher.totalActive++
	}
	return record, local, err
}

func (dispatcher *OutboxDispatcher) deliver(ctx context.Context, record outboxRecord, local bool) error {
	work, cancel := context.WithCancelCause(ctx)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		dispatcher.renewLease(work, cancel, record)
	}()
	raw, err := json.Marshal(record.Event)
	if err == nil {
		if local {
			err = dispatcher.queue.HandleLocally(work, record.TaskCode, record.Event.EventID, raw)
		} else {
			err = dispatcher.queue.PublishEvent(work, record.TaskCode, record.Event.EventID, raw)
		}
	}
	cancel(nil)
	<-stopped
	if cause := context.Cause(work); !errors.Is(cause, context.Canceled) {
		return cause
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !local && streamCapacityExceeded(err) {
		// The broker explicitly refused storage. The handler has never run.
		// Preserve the original ID and attempt budget while operators restore capacity.
		tag, updateErr := dispatcher.db.Exec(ctx, `update nats_outbox set status='failed',available_at=now()+interval '30 seconds',
			last_error=$4,locked_at=null,locked_by='',attempts=attempts-1,updated_at=now()
			where id=$1 and status='publishing' and locked_by=$2 and attempts=$3`, record.ID, dispatcher.workerID,
			record.Attempts+1, errStreamBackpressure.Error())
		if updateErr != nil {
			return updateErr
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("outbox row %d lost publishing ownership under backpressure", record.ID)
		}
		dispatcher.failed.Add(1)
		dispatcher.retried.Add(1)
		return errStreamBackpressure
	}
	if errors.Is(err, ErrTaskDisabled) || errors.Is(err, errDeliveryModeChanged) {
		// A hot reconfiguration raced the claim. Restore the attempt budget;
		// a deliberate pause must not consume retries or create dead letters.
		tag, err := dispatcher.db.Exec(ctx, `update nats_outbox set status='pending',locked_at=null,locked_by='',attempts=attempts-1,updated_at=now()
			where id=$1 and status='publishing' and locked_by=$2 and attempts=$3`, record.ID, dispatcher.workerID, record.Attempts+1)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("outbox row %d lost publishing ownership before release", record.ID)
		}
		return nil
	}
	if err != nil {
		// Once the lease monitor has stopped, classify errors for audited replay.
		// An unavailable local subscription never received the event.
		record.localConsumer = local && !errors.Is(err, ErrUnavailable)
		return dispatcher.fail(ctx, record, err)
	}
	result, err := dispatcher.db.Exec(ctx, `update nats_outbox set status='published',published_at=now(),locked_at=null,locked_by='',updated_at=now()
		where id=$1 and status='publishing' and locked_by=$2 and attempts=$3`, record.ID, dispatcher.workerID, record.Attempts+1)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("outbox row %d lost publishing ownership before completion", record.ID)
	}
	dispatcher.published.Add(1)
	return nil
}

func (dispatcher *OutboxDispatcher) renewLease(ctx context.Context, cancel context.CancelCauseFunc, record outboxRecord) {
	interval := dispatcher.lease / 3
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			update, stop := context.WithTimeout(ctx, interval)
			tag, err := dispatcher.db.Exec(update, `update nats_outbox set locked_at=now(),updated_at=now()
				where id=$1 and status='publishing' and locked_by=$2 and attempts=$3`, record.ID, dispatcher.workerID, record.Attempts+1)
			stop()
			if err != nil {
				cancel(fmt.Errorf("renew publishing lease for row %d: %w", record.ID, err))
				return
			}
			if tag.RowsAffected() != 1 {
				cancel(fmt.Errorf("outbox row %d lost publishing ownership during delivery", record.ID))
				return
			}
		}
	}
}

func (dispatcher *OutboxDispatcher) fail(ctx context.Context, record outboxRecord, cause error) error {
	attempts := record.Attempts + 1
	errorText := diagnostic.Text(cause.Error(), 1000)
	if attempts >= record.MaxAttempts {
		stage := "publish"
		var deadEnvelope any
		if record.localConsumer {
			stage = "consumer:" + record.TaskCode
			raw, err := json.Marshal(record.Event)
			if err != nil {
				return err
			}
			deadEnvelope = string(raw)
		}
		result, err := dispatcher.db.Exec(ctx, `with moved as (
			update nats_outbox set status='dead',last_error=$2,locked_at=null,locked_by='',updated_at=now()
			where id=$1 and status='publishing' and locked_by=$3 and attempts=$4
			returning event_id,event_type,subject,payload,aggregate_type,aggregate_id,attempts
		) insert into dead_letter_events(event_id,event_type,subject,payload,failure_stage,aggregate_type,aggregate_id,attempts,last_error)
		select event_id,event_type,subject,coalesce($6::jsonb,payload),$5,aggregate_type,aggregate_id,attempts,$2 from moved
		on conflict(event_id,failure_stage) do update set
		aggregate_type=excluded.aggregate_type,aggregate_id=excluded.aggregate_id,
		attempts=excluded.attempts,last_error=excluded.last_error,failed_at=now(),replayed_at=null`, record.ID, errorText, dispatcher.workerID, attempts, stage, deadEnvelope)
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
	backoff := jitteredRetryDelay(time.Second * time.Duration(1<<min(attempts, int64(8))))
	result, err := dispatcher.db.Exec(ctx, `update nats_outbox set status='failed',last_error=$2,available_at=now()+$3::interval,
		locked_at=null,locked_by='',updated_at=now() where id=$1 and status='publishing' and locked_by=$4 and attempts=$5`, record.ID, errorText, fmt.Sprintf("%f seconds", backoff.Seconds()), dispatcher.workerID, attempts)
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
