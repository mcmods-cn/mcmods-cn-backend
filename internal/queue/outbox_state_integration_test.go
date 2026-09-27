package queue

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestOutboxStateTransitionsDoNotIgnorePersistenceResults(t *testing.T) {
	source, err := os.ReadFile("outbox.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if strings.Contains(text, "_, _ = tx.Exec(ctx, `update nats_outbox") || strings.Contains(text, "_, _ = dispatcher.db.Exec(ctx, `update nats_outbox") {
		t.Fatal("outbox state transition still ignores its database result")
	}
	if !strings.Contains(text, "func (dispatcher *OutboxDispatcher) fail(ctx context.Context, record outboxRecord, cause error) error") {
		t.Fatal("publish failure persistence cannot report an error to DispatchBatch")
	}
}

func TestOutboxFailureMetricsRequirePersistedTransitions(t *testing.T) {
	pool := openOutboxStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into nats_outbox(id,event_id,event_type,subject,aggregate_type,aggregate_id,payload,status,locked_at,locked_by,attempts,max_attempts)
		values(1,'retry-ok','test','ai','test','retry-ok','{}','publishing',now(),'state-test',1,3),
		      (2,'dead-ok','test','ai','test','dead-ok','{}','publishing',now(),'state-test',3,3),
		      (3,'retry-broken','test','ai','test','retry-broken','{}','publishing',now(),'state-test',1,3),
		      (4,'dead-broken','test','ai','test','dead-broken','{}','publishing',now(),'state-test',3,3)`); err != nil {
		t.Fatal(err)
	}
	dispatcher := &OutboxDispatcher{db: pool, workerID: "state-test"}
	if err := dispatcher.fail(ctx, outboxRecord{ID: 1, Attempts: 0, MaxAttempts: 3}, errors.New("retry me")); err != nil {
		t.Fatal(err)
	}
	assertOutboxStatus(t, ctx, pool, 1, "failed")
	if metrics := dispatcher.Metrics(); metrics.Failed != 1 || metrics.Retried != 1 || metrics.Dead != 0 {
		t.Fatalf("retry metrics = %#v", metrics)
	}
	if err := dispatcher.fail(ctx, outboxRecord{ID: 1, Attempts: 0, MaxAttempts: 3}, errors.New("duplicate owner")); err == nil {
		t.Fatal("lost retry ownership was reported as persisted")
	}
	if metrics := dispatcher.Metrics(); metrics.Failed != 1 || metrics.Retried != 1 {
		t.Fatalf("ownership conflict changed metrics: %#v", metrics)
	}

	if err := dispatcher.fail(ctx, outboxRecord{ID: 2, Attempts: 2, MaxAttempts: 3}, errors.New("dead now")); err != nil {
		t.Fatal(err)
	}
	assertOutboxStatus(t, ctx, pool, 2, "dead")
	var aggregateType, aggregateID string
	if err := pool.QueryRow(ctx, `select aggregate_type,aggregate_id from dead_letter_events
		where event_id='dead-ok' and failure_stage='publish'`).Scan(&aggregateType, &aggregateID); err != nil || aggregateType != "test" || aggregateID != "dead-ok" {
		t.Fatalf("dead-letter aggregate = %q/%q, err=%v", aggregateType, aggregateID, err)
	}
	if metrics := dispatcher.Metrics(); metrics.Failed != 2 || metrics.Retried != 1 || metrics.Dead != 1 {
		t.Fatalf("dead metrics = %#v", metrics)
	}

	if _, err := pool.Exec(ctx, `alter table nats_outbox add constraint reject_broken_retry check(id<>3 or status<>'failed');
		alter table dead_letter_events add constraint reject_broken_dead check(event_id<>'dead-broken')`); err != nil {
		t.Fatal(err)
	}
	before := dispatcher.Metrics()
	if err := dispatcher.fail(ctx, outboxRecord{ID: 3, Attempts: 0, MaxAttempts: 3}, errors.New("retry persistence fails")); err == nil {
		t.Fatal("retry persistence failure was ignored")
	}
	assertOutboxStatus(t, ctx, pool, 3, "publishing")
	if metrics := dispatcher.Metrics(); metrics != before {
		t.Fatalf("failed retry persistence changed metrics: before=%#v after=%#v", before, metrics)
	}
	if err := dispatcher.fail(ctx, outboxRecord{ID: 4, Attempts: 2, MaxAttempts: 3}, errors.New("dead persistence fails")); err == nil {
		t.Fatal("dead-letter persistence failure was ignored")
	}
	assertOutboxStatus(t, ctx, pool, 4, "publishing")
	if metrics := dispatcher.Metrics(); metrics != before {
		t.Fatalf("failed dead persistence changed metrics: before=%#v after=%#v", before, metrics)
	}
}

func TestOutboxStaleLeaseRecoveryErrorsAreReturned(t *testing.T) {
	pool := openOutboxStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into nats_outbox(id,event_id,event_type,subject,aggregate_type,aggregate_id,payload,status,locked_at,locked_by)
		values(10,'stale-broken','test','ai','test','stale-broken','{}','publishing',now()-interval '10 minutes','old-worker');
		alter table nats_outbox add constraint reject_stale_reset check(id<>10 or status<>'failed')`); err != nil {
		t.Fatal(err)
	}
	dispatcher := &OutboxDispatcher{db: pool, workerID: "state-test"}
	if _, err := dispatcher.claim(ctx, 10); err == nil {
		t.Fatal("stale lease recovery persistence error was ignored")
	}
	assertOutboxStatus(t, ctx, pool, 10, "publishing")
}

func TestOutboxStaleLeaseIsRecoveredAfterDispatcherRestart(t *testing.T) {
	pool := openOutboxStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into nats_outbox(id,event_id,event_type,subject,aggregate_type,aggregate_id,payload,status,locked_at,locked_by)
		values(11,'stale-recovered','test','ai','test','stale-recovered','{}','publishing',now()-interval '10 minutes','stopped-worker')`); err != nil {
		t.Fatal(err)
	}
	client := New(ctx, config.NATSConfig{})
	if err := client.SubscribeTask("ai", func(context.Context, []byte) error { return nil }); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected local handler registration on disabled NATS, got %v", err)
	}
	dispatcher := NewOutboxDispatcher(pool, client, true)
	if count, err := dispatcher.DispatchBatch(ctx, 1); err != nil || count != 1 {
		t.Fatalf("restarted dispatcher recovered %d rows: %v", count, err)
	}
	assertOutboxStatus(t, ctx, pool, 11, "published")
	if metrics := dispatcher.Metrics(); metrics.Published != 1 || metrics.Failed != 0 {
		t.Fatalf("unexpected recovery metrics: %#v", metrics)
	}
}

func TestTwoOutboxDispatchersDoNotProcessTheSameClaim(t *testing.T) {
	pool := openOutboxStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into nats_outbox(id,event_id,event_type,subject,aggregate_type,aggregate_id,payload,status)
		values(30,'claim-first','test','ai','test','first','{}','pending'),
		      (31,'claim-second','test','ai','test','second','{}','pending')`); err != nil {
		t.Fatal(err)
	}
	client := New(ctx, config.NATSConfig{})
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	var firstOnce sync.Once
	if err := client.SubscribeTask("ai", func(handlerCtx context.Context, _ []byte) error {
		if EventIDFromContext(handlerCtx) == "claim-first" {
			firstOnce.Do(func() { close(firstEntered) })
			select {
			case <-releaseFirst:
			case <-handlerCtx.Done():
				return handlerCtx.Err()
			}
		}
		return nil
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected local handler registration on disabled NATS, got %v", err)
	}
	firstDispatcher := NewOutboxDispatcher(pool, client, true)
	secondDispatcher := NewOutboxDispatcher(pool, client, true)
	type dispatchResult struct {
		count int
		err   error
	}
	firstResult := make(chan dispatchResult, 1)
	go func() {
		count, err := firstDispatcher.DispatchBatch(ctx, 1)
		firstResult <- dispatchResult{count: count, err: err}
	}()
	select {
	case <-firstEntered:
	case <-ctx.Done():
		t.Fatal("first dispatcher did not enter its claimed handler")
	}
	if count, err := secondDispatcher.DispatchBatch(ctx, 1); err != nil || count != 1 {
		t.Fatalf("second dispatcher result=%d/%v", count, err)
	}
	close(releaseFirst)
	result := <-firstResult
	if result.err != nil || result.count != 1 {
		t.Fatalf("first dispatcher result=%d/%v", result.count, result.err)
	}
	assertOutboxStatus(t, ctx, pool, 30, "published")
	assertOutboxStatus(t, ctx, pool, 31, "published")
	if firstDispatcher.Metrics().Published != 1 || secondDispatcher.Metrics().Published != 1 {
		t.Fatalf("claims were not divided exactly once: first=%#v second=%#v", firstDispatcher.Metrics(), secondDispatcher.Metrics())
	}
}

func TestOutboxPublishedMetricRequiresPersistedTransition(t *testing.T) {
	pool := openOutboxStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into nats_outbox(id,event_id,event_type,subject,aggregate_type,aggregate_id,payload,status)
		values(20,'publish-broken','test','ai','test','publish-broken','{}','pending');
		alter table nats_outbox add constraint reject_published_state check(id<>20 or status<>'published')`); err != nil {
		t.Fatal(err)
	}
	client := New(ctx, config.NATSConfig{})
	_ = client.SubscribeTask("ai", func(context.Context, []byte) error { return nil })
	dispatcher := NewOutboxDispatcher(pool, client, true)
	if count, err := dispatcher.DispatchBatch(ctx, 1); err == nil || count != 1 {
		t.Fatalf("published persistence result = %d/%v, want one claimed row and an error", count, err)
	}
	assertOutboxStatus(t, ctx, pool, 20, "publishing")
	if metrics := dispatcher.Metrics(); metrics.Published != 0 || metrics.Failed != 0 || metrics.Retried != 0 || metrics.Dead != 0 {
		t.Fatalf("unpersisted publish changed metrics: %#v", metrics)
	}
}

func openOutboxStateTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to run outbox state integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
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
			event_id text not null,event_type text not null,subject text not null,payload jsonb not null,failure_stage text not null,
			aggregate_type text not null default '',aggregate_id text not null default '',attempts bigint not null,
			last_error text not null,failed_at timestamptz not null default now(),replayed_at timestamptz,
			unique(event_id,failure_stage))`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	return pool
}

func assertOutboxStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id int64, want string) {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `select status from nats_outbox where id=$1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != want {
		t.Fatalf("outbox %d status = %q, want %q", id, status, want)
	}
}
