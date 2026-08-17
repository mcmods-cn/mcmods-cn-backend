package queue

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestTransactionalOutboxRollbackAndDispatchIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" || os.Getenv("MCMODS_RUN_NATS_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 and MCMODS_RUN_NATS_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	appConfig := config.Load()
	pool, err := pgxpool.New(ctx, appConfig.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	prefix := "mcmods-outbox-test-" + time.Now().Format("150405000")
	stream := "MCMODS_OUTBOX_TEST_" + time.Now().Format("150405000")
	client := New(ctx, config.NATSConfig{Enabled: true, URL: os.Getenv("MCMODS_TEST_NATS_URL"), SubjectPrefix: prefix,
		JetStream: config.JetStreamConfig{Enabled: true, Stream: stream, MaxDeliver: 3, AckWait: time.Second, PublishTimeout: 2 * time.Second},
		Tasks: []config.NATSTaskConfig{
			{Code: "outbox_test", Enabled: true, Subject: "outbox.test", QueueGroup: "outbox-test-workers", MaxConcurrent: 1, TimeoutSeconds: 2},
			{Code: "dead_test", Enabled: true, Subject: "dead.test", QueueGroup: "dead-test-workers", MaxConcurrent: 1, TimeoutSeconds: 2},
		}})
	client.SetDeadLetterSink(NewPostgresDeadLetterSink(pool))
	defer client.Close()
	defer func() {
		client.mu.RLock()
		js := client.jetStream
		client.mu.RUnlock()
		if js != nil {
			_ = js.DeleteStream(stream)
		}
	}()
	delivered := make(chan struct{}, 1)
	if err = client.SubscribeTask("outbox_test", func(context.Context, []byte) error {
		select {
		case delivered <- struct{}{}:
		default:
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rolledBackID, err := EnqueueTx(ctx, tx, "outbox_test", "test.rolled_back", "integration_test", prefix+"-rollback", "", map[string]bool{"ok": false})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var rollbackCount int
	if err = pool.QueryRow(ctx, `select count(*) from nats_outbox where event_id=$1`, rolledBackID).Scan(&rollbackCount); err != nil || rollbackCount != 0 {
		t.Fatalf("rolled back event persisted: count=%d err=%v", rollbackCount, err)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eventID, err := EnqueueTx(ctx, tx, "outbox_test", "test.committed", "integration_test", prefix, "trace-test", map[string]bool{"ok": true})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from nats_outbox where aggregate_type='integration_test' and aggregate_id like $1`, prefix+"%")
	})
	dispatcher := NewOutboxDispatcher(pool, client, true)
	if count, dispatchErr := dispatcher.DispatchBatch(ctx, 16); dispatchErr != nil || count < 1 {
		t.Fatalf("dispatch count=%d err=%v", count, dispatchErr)
	}
	select {
	case <-delivered:
	case <-ctx.Done():
		t.Fatal("committed outbox event was not consumed")
	}
	var status string
	var publishedAt *time.Time
	if err = pool.QueryRow(ctx, `select status,published_at from nats_outbox where event_id=$1`, eventID).Scan(&status, &publishedAt); err != nil || status != "published" || publishedAt == nil {
		t.Fatalf("outbox status=%q published=%v err=%v", status, publishedAt, err)
	}
	deadEnvelope := EventEnvelope{EventID: randomEventID(), EventType: "test.consumer_dead", SchemaVersion: 1, OccurredAt: time.Now(), AggregateType: "test", AggregateID: prefix, Payload: []byte(`{"dead":true}`)}
	deadRaw, _ := json.Marshal(deadEnvelope)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from dead_letter_events where event_id=$1`, deadEnvelope.EventID)
	})
	if err = client.SubscribeTask("dead_test", func(context.Context, []byte) error { return errors.New("permanent consumer failure") }); err != nil {
		t.Fatal(err)
	}
	if err = client.PublishEvent(ctx, "dead_test", deadEnvelope.EventID, deadRaw); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var attempts int
		err = pool.QueryRow(ctx, `select attempts from dead_letter_events where event_id=$1 and failure_stage='consumer:dead_test'`, deadEnvelope.EventID).Scan(&attempts)
		if err == nil {
			if attempts != 3 {
				t.Fatalf("dead letter attempts=%d, want 3", attempts)
			}
			break
		}
		if !errors.Is(err, pgx.ErrNoRows) || time.Now().After(deadline) {
			t.Fatalf("consumer dead letter was not persisted: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
