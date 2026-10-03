package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/config"
)

func TestTransactionalOutboxRollbackAndDispatchIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool := openOutboxStateTestDB(t)
	prefix := "mcmods-outbox-test-" + randomEventID()[:16]
	stream := uniqueJetStreamName("MCMODS_OUTBOX_TEST_")
	client := New(ctx, config.NATSConfig{Enabled: true, URL: jetStreamIntegrationURL(t), SubjectPrefix: prefix,
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
	if err := client.SubscribeTask("outbox_test", func(context.Context, []byte) error {
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
	defer tx.Rollback(ctx)
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
	defer tx.Rollback(ctx)
	eventID, err := EnqueueTx(ctx, tx, "outbox_test", "test.committed", "integration_test", prefix, "trace-test", map[string]bool{"ok": true})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
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
	if err = client.SubscribeTask("dead_test", func(context.Context, []byte) error { return errors.New("permanent consumer failure") }); err != nil {
		t.Fatal(err)
	}
	if err = client.PublishEvent(ctx, "dead_test", deadEnvelope.EventID, deadRaw); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var attempts int
		var aggregateType, aggregateID string
		err = pool.QueryRow(ctx, `select attempts,aggregate_type,aggregate_id from dead_letter_events
			where event_id=$1 and failure_stage='consumer:dead_test'`, deadEnvelope.EventID).
			Scan(&attempts, &aggregateType, &aggregateID)
		if err == nil {
			if attempts != 3 || aggregateType != deadEnvelope.AggregateType || aggregateID != deadEnvelope.AggregateID {
				t.Fatalf("dead letter attempts=%d aggregate=%q/%q", attempts, aggregateType, aggregateID)
			}
			break
		}
		if !errors.Is(err, pgx.ErrNoRows) || time.Now().After(deadline) {
			t.Fatalf("consumer dead letter was not persisted: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestOutboxRetriesAfterJetStreamDisconnectIntegration(t *testing.T) {
	pool := openOutboxStateTestDB(t)
	harness := newRestartableJetStreamServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stream := uniqueJetStreamName("MCMODS_OUTBOX_RECONNECT_")
	client := New(ctx, config.NATSConfig{
		Enabled: true, URL: harness.url(), SubjectPrefix: "mcmods-outbox-reconnect-" + randomEventID()[:16],
		JetStream: config.JetStreamConfig{Enabled: true, Stream: stream, MaxDeliver: 3, AckWait: 200 * time.Millisecond, PublishTimeout: time.Second},
		Tasks:     []config.NATSTaskConfig{{Code: "disconnect_test", Enabled: true, Subject: "disconnect.test", QueueGroup: "disconnect-test-workers", MaxConcurrent: 1, TimeoutSeconds: 2}},
	})
	defer client.Close()
	delivered := make(chan struct{}, 1)
	if err := client.SubscribeTask("disconnect_test", func(context.Context, []byte) error {
		delivered <- struct{}{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	eventID, err := EnqueueTx(ctx, tx, "disconnect_test", "test.disconnect", "integration_test", "disconnect", "", map[string]bool{"ok": true})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	harness.stop()
	waitForNATSStatus(t, client, false)
	dispatcher := NewOutboxDispatcher(pool, client, true)
	if count, dispatchErr := dispatcher.DispatchBatch(ctx, 1); dispatchErr != nil || count != 1 {
		t.Fatalf("disconnect dispatch result=%d/%v", count, dispatchErr)
	}
	var status, lastError string
	if err = pool.QueryRow(ctx, `select status,last_error from nats_outbox where event_id=$1`, eventID).Scan(&status, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || lastError == "" {
		t.Fatalf("disconnect was not persisted as retryable failure: status=%q error=%q", status, lastError)
	}
	if metrics := dispatcher.Metrics(); metrics.Failed != 1 || metrics.Retried != 1 || metrics.Published != 0 {
		t.Fatalf("unexpected disconnect metrics: %#v", metrics)
	}

	harness.start(t)
	waitForNATSStatus(t, client, true)
	if _, err = pool.Exec(ctx, `update nats_outbox set available_at=now() where event_id=$1`, eventID); err != nil {
		t.Fatal(err)
	}
	if count, dispatchErr := dispatcher.DispatchBatch(ctx, 1); dispatchErr != nil || count != 1 {
		t.Fatalf("reconnect dispatch result=%d/%v", count, dispatchErr)
	}
	select {
	case <-delivered:
	case <-ctx.Done():
		t.Fatal("reconnected JetStream event was not consumed")
	}
	if err = pool.QueryRow(ctx, `select status from nats_outbox where event_id=$1`, eventID).Scan(&status); err != nil || status != "published" {
		t.Fatalf("reconnected outbox status=%q err=%v", status, err)
	}
	if metrics := dispatcher.Metrics(); metrics.Failed != 1 || metrics.Retried != 1 || metrics.Published != 1 {
		t.Fatalf("unexpected reconnect metrics: %#v", metrics)
	}
}
