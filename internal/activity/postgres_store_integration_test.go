package activity

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

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
		monitors[index] = NewMonitorWithOptions(pool, nil, options)
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
