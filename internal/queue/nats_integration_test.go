package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestTaskMaxConcurrentIsTheAuthoritativeExecutionLimitIntegration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := uniqueJetStreamName("MCMODS_CONCURRENCY_")
	client := New(ctx, config.NATSConfig{
		Enabled: true, URL: jetStreamIntegrationURL(t), SubjectPrefix: "mcmods-concurrency-" + randomEventID()[:16],
		JetStream: config.JetStreamConfig{Enabled: true, Stream: stream, MaxDeliver: 3, AckWait: time.Second, PublishTimeout: 2 * time.Second},
		Tasks: []config.NATSTaskConfig{{
			Code: "ai", Enabled: true, Subject: "ai.tasks", QueueGroup: "mcmods-concurrency-workers", MaxConcurrent: 2, TimeoutSeconds: 5,
		}},
	})
	defer client.Close()
	defer func() {
		client.mu.RLock()
		js := client.jetStream
		client.mu.RUnlock()
		if js != nil {
			_ = js.DeleteStream(stream)
		}
	}()

	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	started := make(chan struct{}, 4)
	finished := make(chan struct{}, 4)
	var active atomic.Int32
	var maximum atomic.Int32
	if err := client.SubscribeTask("ai", func(context.Context, []byte) error {
		current := active.Add(1)
		for observed := maximum.Load(); current > observed && !maximum.CompareAndSwap(observed, current); observed = maximum.Load() {
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
		finished <- struct{}{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 4; index++ {
		publishEnvelope(t, ctx, client, "ai", fmt.Sprintf("concurrency-%d", index), `{"ok":true}`)
	}
	for index := 0; index < 2; index++ {
		select {
		case <-started:
		case <-time.After(4 * time.Second):
			t.Fatal("configured concurrent handlers did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("a third AI handler started above the NATS task concurrency limit")
	case <-time.After(300 * time.Millisecond):
	}
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum active handlers = %d, want 2", got)
	}
	releaseOnce.Do(func() { close(release) })
	for index := 0; index < 4; index++ {
		select {
		case <-finished:
		case <-time.After(4 * time.Second):
			t.Fatal("queued handler did not finish after capacity was released")
		}
	}
}

func TestJetStreamExplicitAckRedeliveryAndDeduplicationIntegration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := uniqueJetStreamName("MCMODS_TEST_")
	url := jetStreamIntegrationURL(t)
	client := New(ctx, config.NATSConfig{
		Enabled: true, URL: url, SubjectPrefix: "mcmods-test-" + randomEventID()[:16],
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
	client.mu.RLock()
	js := client.jetStream
	client.mu.RUnlock()
	consumer, err := js.ConsumerInfo(stream, cleanDurable("mcmods-test-workers"))
	if err != nil {
		t.Fatal(err)
	}
	if consumer.Config.MaxDeliver != 3 || len(consumer.Config.BackOff) != 3 || consumer.Config.BackOff[0] != 200*time.Millisecond || consumer.Config.BackOff[1] != 400*time.Millisecond {
		t.Fatalf("unexpected durable retry policy: %+v", consumer.Config)
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

func TestJetStreamDurableConsumerResumesBacklogAfterRestartIntegration(t *testing.T) {
	url := jetStreamIntegrationURL(t)
	stream := uniqueJetStreamName("MCMODS_RESTART_")
	prefix := "mcmods-restart-" + randomEventID()[:16]
	cfg := config.NATSConfig{
		Enabled: true, URL: url, SubjectPrefix: prefix,
		JetStream: config.JetStreamConfig{Enabled: true, Stream: stream, MaxDeliver: 3, AckWait: 300 * time.Millisecond, PublishTimeout: 2 * time.Second},
		Tasks:     []config.NATSTaskConfig{{Code: "resume", Enabled: true, Subject: "resume.tasks", QueueGroup: "mcmods-resume-workers", MaxConcurrent: 1, TimeoutSeconds: 2}},
	}
	firstContext, cancelFirst := context.WithCancel(context.Background())
	first := New(firstContext, cfg)
	firstDelivery := make(chan string, 1)
	if err := first.SubscribeTask("resume", func(_ context.Context, payload []byte) error {
		firstDelivery <- string(payload)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	publishEnvelope(t, firstContext, first, "resume", "first", `{"sequence":1}`)
	select {
	case payload := <-firstDelivery:
		if payload != `{"sequence":1}` {
			t.Fatalf("unexpected first payload %s", payload)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("first durable delivery timed out")
	}
	first.Close()
	cancelFirst()

	publisherContext, cancelPublisher := context.WithCancel(context.Background())
	publisher := New(publisherContext, cfg)
	publishEnvelope(t, publisherContext, publisher, "resume", "backlog", `{"sequence":2}`)
	publisher.Close()
	cancelPublisher()

	secondContext, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	second := New(secondContext, cfg)
	defer second.Close()
	defer func() {
		second.mu.RLock()
		js := second.jetStream
		second.mu.RUnlock()
		if js != nil {
			_ = js.DeleteStream(stream)
		}
	}()
	secondDelivery := make(chan string, 2)
	if err := second.SubscribeTask("resume", func(_ context.Context, payload []byte) error {
		secondDelivery <- string(payload)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case payload := <-secondDelivery:
		if payload != `{"sequence":2}` {
			t.Fatalf("durable restart replayed the wrong payload %s", payload)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("offline durable backlog was not delivered after consumer restart")
	}
	select {
	case duplicate := <-secondDelivery:
		t.Fatalf("acknowledged pre-restart message was delivered again: %s", duplicate)
	case <-time.After(400 * time.Millisecond):
	}
}

func TestJetStreamReconnectsAfterServerRestartIntegration(t *testing.T) {
	harness := newRestartableJetStreamServer(t)
	stream := uniqueJetStreamName("MCMODS_RECONNECT_")
	cfg := config.NATSConfig{
		Enabled: true, URL: harness.url(), SubjectPrefix: "mcmods-reconnect-" + randomEventID()[:16],
		JetStream: config.JetStreamConfig{Enabled: true, Stream: stream, MaxDeliver: 3, AckWait: 300 * time.Millisecond, PublishTimeout: 2 * time.Second},
		Tasks:     []config.NATSTaskConfig{{Code: "reconnect", Enabled: true, Subject: "reconnect.tasks", QueueGroup: "mcmods-reconnect-workers", MaxConcurrent: 1, TimeoutSeconds: 2}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := New(ctx, cfg)
	defer client.Close()
	delivered := make(chan struct{}, 1)
	if err := client.SubscribeTask("reconnect", func(context.Context, []byte) error {
		delivered <- struct{}{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	harness.stop()
	waitForNATSStatus(t, client, false)
	harness.start(t)
	waitForNATSStatus(t, client, true)
	publishEnvelope(t, ctx, client, "reconnect", "after-restart", `{"ok":true}`)
	select {
	case <-delivered:
	case <-time.After(5 * time.Second):
		t.Fatal("subscription did not resume after NATS server restart")
	}
}

func publishEnvelope(t *testing.T, ctx context.Context, client *Client, taskCode, eventID, payload string) {
	t.Helper()
	envelope := EventEnvelope{
		EventID: eventID + "-" + randomEventID(), EventType: "test." + eventID,
		SchemaVersion: 1, OccurredAt: time.Now().UTC(), AggregateType: "test", AggregateID: eventID,
		Payload: json.RawMessage(payload),
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.PublishEvent(ctx, taskCode, envelope.EventID, raw); err != nil {
		t.Fatal(err)
	}
}

func waitForNATSStatus(t *testing.T, client *Client, connected bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if client.Status().Connected == connected {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("NATS connected status did not become %t: %#v", connected, client.Status())
}
