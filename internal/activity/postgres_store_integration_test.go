package activity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

type replayProjectionProcessor struct {
	fail      bool
	attempts  int
	committed int
}

func (processor *replayProjectionProcessor) ProcessActivityBatch(context.Context, []Event) error {
	return errors.New("durable projection used the non-transactional processor")
}

func (processor *replayProjectionProcessor) ProcessActivityBatchTx(ctx context.Context, tx pgx.Tx, events []Event) error {
	processor.attempts++
	if processor.fail {
		return errors.New("injected progression failure")
	}
	for _, event := range events {
		if _, err := tx.Exec(ctx, `insert into durable_projection_probe(user_id,projected)
			values($1,1) on conflict(user_id) do update
			set projected=durable_projection_probe.projected+1`, event.UserID); err != nil {
			return err
		}
	}
	return nil
}

func (processor *replayProjectionProcessor) ActivityBatchCommitted(context.Context, []Event) {
	processor.committed++
}

func TestDurableOutboxProjectionFailureRollsBackAndReplaysIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify atomic durable activity projection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()
	if _, err = pool.Exec(ctx, `create temporary table durable_projection_probe(
		user_id bigint primary key,projected integer not null)`); err != nil {
		t.Fatal(err)
	}

	var userID int64
	suffix := time.Now().UnixNano()
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'projection-replay-test',true) returning id`,
		fmt.Sprintf("projection-replay-%d", suffix), fmt.Sprintf("projection-replay-%d@example.invalid", suffix)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	processor := &replayProjectionProcessor{fail: true}
	store := newPostgresStore(pool, processor)
	if err = store.EnqueueDurable(ctx, Event{
		UserID: userID, ActionID: ActionCreate, ObjectTypeID: ObjectUser, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	if count, drainErr := store.DrainDurable(ctx, 10); drainErr == nil || count != 0 {
		t.Fatalf("failed projection drained count=%d err=%v; want 0/error", count, drainErr)
	}
	assertDurableActivityState(t, ctx, pool, userID, 1, 0)
	assertDurableProjectionState(t, ctx, pool, userID, 0)
	if processor.committed != 0 {
		t.Fatalf("post-commit hook called %d times after rollback", processor.committed)
	}
	if processor.attempts != 1 {
		t.Fatalf("transactional processor called %d times after failure; want 1", processor.attempts)
	}

	processor.fail = false
	if _, err = pool.Exec(ctx, `update activity_event_outbox set available_at=clock_timestamp() where user_id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if count, drainErr := store.DrainDurable(ctx, 10); drainErr != nil || count != 1 {
		t.Fatalf("replayed projection drained count=%d err=%v; want 1/nil", count, drainErr)
	}
	assertDurableActivityState(t, ctx, pool, userID, 0, 1)
	assertDurableProjectionState(t, ctx, pool, userID, 1)
	if processor.committed != 1 {
		t.Fatalf("post-commit hook called %d times; want 1", processor.committed)
	}
	if processor.attempts != 2 {
		t.Fatalf("transactional processor called %d times after replay; want 2", processor.attempts)
	}
}

func assertDurableActivityState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64, wantOutbox, wantRaw int) {
	t.Helper()
	var outboxRows, rawRows int
	if err := pool.QueryRow(ctx, `select
		(select count(*) from activity_event_outbox where user_id=$1),
		(select count(*) from user_activity_events where user_id=$1)`, userID).Scan(&outboxRows, &rawRows); err != nil {
		t.Fatal(err)
	}
	if outboxRows != wantOutbox || rawRows != wantRaw {
		t.Fatalf("durable state outbox=%d raw=%d; want %d/%d", outboxRows, rawRows, wantOutbox, wantRaw)
	}
}

func assertDurableProjectionState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64, want int) {
	t.Helper()
	var projected int
	if err := pool.QueryRow(ctx, `select coalesce(max(projected),0) from durable_projection_probe where user_id=$1`, userID).Scan(&projected); err != nil {
		t.Fatal(err)
	}
	if projected != want {
		t.Fatalf("durable projection=%d; want %d", projected, want)
	}
}

func TestDurableOutboxSurvivesProducerAndDrainsExactlyOnceAcrossWorkers(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an isolated generation-70 PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	suffix := time.Now().UnixNano()
	var userID, routeID int64
	err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'integration-test',true) returning id`,
		fmt.Sprintf("activity-it-%d", suffix), fmt.Sprintf("activity-it-%d@example.invalid", suffix)).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `delete from user_activity_events where user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `delete from users where id=$1`, userID)
	}()
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='user' and internal_id=$1`, userID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}

	producer := newPostgresStore(pool, nil)
	const eventCount = 20
	for index := range eventCount {
		err = producer.EnqueueDurable(ctx, Event{
			UserID: userID, ActionID: ActionCreate, ObjectTypeID: ObjectUser,
			ObjectRouteID: routeID, MarkdownAddedBytes: index, OccurredAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var queued int
	if err = pool.QueryRow(ctx, `select count(*) from activity_event_outbox where user_id=$1`, userID).Scan(&queued); err != nil || queued != eventCount {
		t.Fatalf("durable queue count=%d err=%v", queued, err)
	}

	var drained atomic.Int64
	var workers sync.WaitGroup
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			consumer := newPostgresStore(pool, nil)
			for {
				count, drainErr := consumer.DrainDurable(ctx, 5)
				if drainErr != nil {
					t.Errorf("drain durable outbox: %v", drainErr)
					return
				}
				drained.Add(int64(count))
				if count == 0 {
					return
				}
			}
		}()
	}
	workers.Wait()
	if drained.Load() != eventCount {
		t.Fatalf("drained %d events, want %d", drained.Load(), eventCount)
	}
	var remaining, persisted, statisticsCount int
	if err = pool.QueryRow(ctx, `select count(*) from activity_event_outbox where user_id=$1`, userID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from user_activity_events where user_id=$1 and action_id=$2`, userID, ActionCreate).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select coalesce((action_counts->>'create')::int,0) from user_statistics_totals where user_id=$1`, userID).Scan(&statisticsCount); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 || persisted != eventCount || statisticsCount != eventCount {
		t.Fatalf("remaining=%d persisted=%d statistics=%d", remaining, persisted, statisticsCount)
	}
}

func TestDurableOutboxConcurrentLoadIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_ACTIVITY_LOAD") != "1" {
		t.Skip("set MCMODS_RUN_ACTIVITY_LOAD=1 only against an authorized development/test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 12
	poolConfig.MinConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	suffix := time.Now().UnixNano()
	var userID, routeID int64
	err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'activity-load-test',true) returning id`,
		fmt.Sprintf("activity-load-%d", suffix), fmt.Sprintf("activity-load-%d@example.invalid", suffix)).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='user' and internal_id=$1`, userID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `delete from user_activity_events where user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `delete from users where id=$1`, userID)
	}()

	options := Options{
		BatchSize: 256, QueueCapacity: 4096, FlushInterval: 20 * time.Millisecond,
		RetryMinDelay: 20 * time.Millisecond, RetryMaxDelay: time.Second,
		WriteTimeout: 10 * time.Second, DurableEnqueueTimeout: 5 * time.Second,
	}
	monitors := make([]*Monitor, 4)
	for index := range monitors {
		monitors[index] = NewMonitor(pool, nil, options)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		for _, monitor := range monitors {
			_ = monitor.Close(closeCtx)
		}
	}()

	const eventCount = 2000
	startedAt := time.Now()
	jobs := make(chan int)
	errCh := make(chan error, 32)
	var producers sync.WaitGroup
	for worker := range 20 {
		producers.Add(1)
		go func(workerID int) {
			defer producers.Done()
			for sequence := range jobs {
				event := Event{UserID: userID, ActionID: ActionEdit, ObjectTypeID: ObjectUser,
					ObjectRouteID: routeID, MarkdownAddedBytes: sequence % 100, OccurredAt: time.Now().UTC()}
				if enqueueErr := monitors[workerID%len(monitors)].RecordDurable(ctx, event); enqueueErr != nil {
					select {
					case errCh <- enqueueErr:
					default:
					}
					return
				}
			}
		}(worker)
	}
	for sequence := range eventCount {
		jobs <- sequence
	}
	close(jobs)
	producers.Wait()
	close(errCh)
	for enqueueErr := range errCh {
		t.Fatalf("concurrent durable enqueue failed: %v", enqueueErr)
	}
	enqueueDuration := time.Since(startedAt)

	deadline := time.Now().Add(45 * time.Second)
	var persisted, queued int
	for time.Now().Before(deadline) {
		if err = pool.QueryRow(ctx, `select count(*) from user_activity_events where user_id=$1 and action_id=$2`, userID, ActionEdit).Scan(&persisted); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `select count(*) from activity_event_outbox where user_id=$1`, userID).Scan(&queued); err != nil {
			t.Fatal(err)
		}
		if persisted == eventCount && queued == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if persisted != eventCount || queued != 0 {
		t.Fatalf("load drain incomplete: persisted=%d queued=%d", persisted, queued)
	}
	totalDuration := time.Since(startedAt)
	t.Logf("durable activity load: events=%d producers=20 consumers=4 enqueue=%s enqueue_rate=%.2f/s total=%s total_rate=%.2f/s",
		eventCount, enqueueDuration, float64(eventCount)/enqueueDuration.Seconds(), totalDuration, float64(eventCount)/totalDuration.Seconds())
}
