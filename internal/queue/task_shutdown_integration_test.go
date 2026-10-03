package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestClosingQueueCancelsRunningTaskIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream := uniqueJetStreamName("OCT02_TASK_CLOSE_")
	cfg := config.NATSConfig{Enabled: true, URL: jetStreamIntegrationURL(t), SubjectPrefix: "close-task-" + randomEventID(),
		JetStream: config.JetStreamConfig{Enabled: true, Stream: stream, AckWait: time.Second, PublishTimeout: time.Second},
		Tasks:     []config.NATSTaskConfig{{Code: "test", Enabled: true, Subject: "test.tasks", QueueGroup: "close-test", MaxConcurrent: 1, TimeoutSeconds: 60}}}
	client := New(ctx, cfg)
	defer client.Close()
	client.mu.RLock()
	js := client.jetStream
	client.mu.RUnlock()
	if js == nil {
		t.Fatal("owned JetStream server unavailable")
	}
	t.Cleanup(func() { _ = js.DeleteStream(stream) })
	started, stopped := make(chan struct{}), make(chan struct{})
	if err := client.SubscribeTask("test", func(taskCtx context.Context, _ []byte) error {
		close(started)
		<-taskCtx.Done()
		close(stopped)
		return taskCtx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	envelope := EventEnvelope{EventID: "synthetic-shutdown", EventType: "synthetic.task", SchemaVersion: 1,
		OccurredAt: time.Now(), AggregateType: "synthetic", AggregateID: "shutdown", Payload: json.RawMessage(`{}`)}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.PublishEvent(ctx, "test", envelope.EventID, raw); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("task did not start")
	}
	client.Close()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("running task retained an independent background context after queue shutdown")
	}
}
