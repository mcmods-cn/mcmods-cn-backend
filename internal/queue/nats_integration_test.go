package queue

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestJetStreamExplicitAckRedeliveryAndDeduplicationIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_NATS_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_NATS_INTEGRATION=1")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := "MCMODS_TEST_" + time.Now().Format("150405000")
	client := New(ctx, config.NATSConfig{
		Enabled: true, URL: os.Getenv("MCMODS_TEST_NATS_URL"), SubjectPrefix: "mcmods-test-" + time.Now().Format("150405000"),
		JetStream: config.JetStreamConfig{Enabled: true, Stream: stream, MaxDeliver: 3, AckWait: 200 * time.Millisecond, PublishTimeout: 2 * time.Second},
		Tasks: []config.NATSTaskConfig{
			{Code: "test", Enabled: true, Subject: "test.tasks", QueueGroup: "mcmods-test-workers", MaxConcurrent: 1, TimeoutSeconds: 2},
			{Code: "dead", Enabled: true, Subject: "dead.tasks", QueueGroup: "mcmods-dead-workers", MaxConcurrent: 1, TimeoutSeconds: 2},
		},
	})
	defer client.Close()
	if status := client.Status(); !status.Connected || !status.JetStream {
		t.Fatalf("JetStream unavailable: %+v", status)
	}
	defer func() {
		client.mu.RLock()
		js := client.jetStream
		client.mu.RUnlock()
		if js != nil {
			_ = js.DeleteStream(stream)
		}
	}()
	var attempts atomic.Int32
	completed := make(chan struct{}, 1)
	if err := client.SubscribeTask("test", func(context.Context, []byte) error {
		if attempts.Add(1) == 1 {
			return errors.New("retry once")
		}
		select {
		case completed <- struct{}{}:
		default:
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	envelope := EventEnvelope{EventID: "event-fixed", EventType: "test.created", SchemaVersion: 1, OccurredAt: time.Now().UTC(), AggregateType: "test", AggregateID: "1", Payload: []byte(`{"ok":true}`)}
	raw, _ := json.Marshal(envelope)
	if err := client.PublishEvent(ctx, "test", envelope.EventID, raw); err != nil {
		t.Fatal(err)
	}
	select {
	case <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("message was not redelivered after NAK")
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts=%d, want 2", attempts.Load())
	}
	if err := client.PublishEvent(ctx, "test", envelope.EventID, raw); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if attempts.Load() != 2 {
		t.Fatalf("duplicate message was delivered, attempts=%d", attempts.Load())
	}
	deadLetters := make(chan DeadLetter, 1)
	client.SetDeadLetterSink(func(_ context.Context, dead DeadLetter) error {
		deadLetters <- dead
		return nil
	})
	if err := client.SubscribeTask("dead", func(context.Context, []byte) error { return errors.New("permanent") }); err != nil {
		t.Fatal(err)
	}
	deadEvent := EventEnvelope{EventID: randomEventID(), EventType: "test.dead", SchemaVersion: 1, OccurredAt: time.Now(), AggregateType: "test", AggregateID: "dead", Payload: json.RawMessage(`{"dead":true}`)}
	deadRaw, _ := json.Marshal(deadEvent)
	if err := client.PublishEvent(ctx, "dead", deadEvent.EventID, deadRaw); err != nil {
		t.Fatal(err)
	}
	select {
	case dead := <-deadLetters:
		if dead.Attempts != 3 || dead.EventID != deadEvent.EventID || dead.FailureStage != "consumer:dead" {
			t.Fatalf("unexpected dead letter: %+v", dead)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("maximum delivery did not create a dead letter")
	}
}
