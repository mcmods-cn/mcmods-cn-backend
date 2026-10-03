package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestTimedOutTaskCanPersistDeadLetterIntegration(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := uniqueJetStreamName("OCT02_TIMEOUT_")
	client := New(parent, config.NATSConfig{
		Enabled: true, URL: jetStreamIntegrationURL(t), SubjectPrefix: "oct02-timeout-" + randomEventID()[:16],
		JetStream: config.JetStreamConfig{Enabled: true, Stream: stream, MaxDeliver: 1, AckWait: 2 * time.Second},
		Tasks:     []config.NATSTaskConfig{{Code: "ai", Enabled: true, Subject: "ai.tasks", QueueGroup: "timeout-workers", MaxConcurrent: 1, TimeoutSeconds: 1}},
	})
	defer client.Close()
	t.Cleanup(func() {
		client.mu.RLock()
		js := client.jetStream
		client.mu.RUnlock()
		if js != nil {
			_ = js.DeleteStream(stream)
		}
	})
	type result struct {
		err     error
		eventID string
		bounded bool
	}
	written := make(chan result, 1)
	client.SetDeadLetterSink(func(ctx context.Context, letter DeadLetter) error {
		deadline, exists := ctx.Deadline()
		written <- result{ctx.Err(), letter.EventID, exists && time.Until(deadline) > 0 && time.Until(deadline) <= 3*time.Second}
		return ctx.Err()
	})
	if err := client.SubscribeTask("ai", func(ctx context.Context, _ []byte) error {
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(EventEnvelope{EventID: "timeout-event", EventType: "test.timeout", SchemaVersion: 1,
		OccurredAt: time.Now().UTC(), AggregateType: "test", AggregateID: "timeout", Payload: json.RawMessage(`{"synthetic":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err = client.PublishEvent(parent, "ai", "timeout-event", raw); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-written:
		if got.err != nil || !got.bounded || got.eventID != "timeout-event" {
			t.Fatalf("dead-letter persistence must get a live bounded context and original event ID: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed-out task did not reach the dead-letter sink")
	}
}
