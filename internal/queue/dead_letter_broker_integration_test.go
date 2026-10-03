package queue

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestDeadLetterIsRetainedByBrokerWhenDatabaseSinkFailsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	stream := uniqueJetStreamName("OCT02_DEAD_BROKER_")
	cfg := config.NATSConfig{Enabled: true, URL: jetStreamIntegrationURL(t), SubjectPrefix: "dead-broker-" + randomEventID(),
		JetStream: config.JetStreamConfig{Enabled: true, Stream: stream, MaxDeliver: 1, AckWait: time.Second, PublishTimeout: time.Second},
		Tasks:     []config.NATSTaskConfig{{Code: "test", Enabled: true, Subject: "test.tasks", QueueGroup: "test-workers", MaxConcurrent: 1, TimeoutSeconds: 1}}}
	client := New(ctx, cfg)
	defer client.Close()
	client.mu.RLock()
	js := client.jetStream
	client.mu.RUnlock()
	t.Cleanup(func() {
		if js != nil {
			_ = js.DeleteStream(stream)
		}
	})
	if js == nil {
		t.Fatal("owned JetStream server unavailable")
	}
	sinkCalled := make(chan struct{}, 1)
	client.SetDeadLetterSink(func(context.Context, DeadLetter) error {
		select {
		case sinkCalled <- struct{}{}:
		default:
		}
		return errors.New("synthetic database unavailable")
	})
	if err := client.SubscribeTask("test", func(context.Context, []byte) error { return errors.New("synthetic terminal task failure") }); err != nil {
		t.Fatal(err)
	}
	envelope := EventEnvelope{EventID: "synthetic-dead-event", EventType: "test.failed", SchemaVersion: 1, OccurredAt: time.Now(),
		AggregateType: "test", AggregateID: "synthetic", Payload: json.RawMessage(`{"synthetic":true}`)}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.PublishEvent(ctx, "test", envelope.EventID, raw); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sinkCalled:
	case <-ctx.Done():
		t.Fatal("terminal task did not reach the persistence sink")
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		message, err := js.GetLastMsg(stream, fullSubject(cfg.SubjectPrefix, "dead-letter.test"))
		if err == nil {
			if string(message.Data) != string(raw) {
				t.Fatal("broker recovery record lost its original event envelope")
			}
			if message.Header.Get("Nats-Msg-Id") == envelope.EventID {
				t.Fatal("dead-letter publication reused the original deduplication ID")
			}
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("database sink failure left no durable broker dead-letter recovery record")
		}
	}
}

func TestDeadLetterDatabaseRecoverySurvivesWorkerRestartIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	stream := uniqueJetStreamName("OCT02_DEAD_RECOVER_")
	cfg := config.NATSConfig{Enabled: true, URL: jetStreamIntegrationURL(t), SubjectPrefix: "dead-recover-" + randomEventID(),
		JetStream: config.JetStreamConfig{Enabled: true, Stream: stream, MaxDeliver: 1, AckWait: time.Second, PublishTimeout: time.Second},
		Tasks:     []config.NATSTaskConfig{{Code: "ai", Enabled: true, Subject: "ai.tasks", QueueGroup: "dead-recover-ai", MaxConcurrent: 1, TimeoutSeconds: 1}}}
	first := New(ctx, cfg)
	defer first.Close()
	first.mu.RLock()
	js := first.jetStream
	first.mu.RUnlock()
	if js == nil {
		t.Fatal("owned JetStream server unavailable")
	}
	t.Cleanup(func() { _ = js.DeleteStream(stream) })
	unavailable := make(chan struct{}, 1)
	first.SetDeadLetterSink(func(context.Context, DeadLetter) error {
		select {
		case unavailable <- struct{}{}:
		default:
		}
		return errors.New("synthetic database outage")
	})
	var handlerCalls atomic.Int32
	handler := func(context.Context, []byte) error {
		handlerCalls.Add(1)
		return errors.New("synthetic provider rejected task")
	}
	if err := first.SubscribeTask("ai", handler); err != nil {
		t.Fatal(err)
	}
	event := EventEnvelope{EventID: "synthetic-recover-event", EventType: "ai.synthetic", SchemaVersion: 1, OccurredAt: time.Now(),
		AggregateType: "synthetic", AggregateID: "recovery", Payload: json.RawMessage(`{"synthetic":true}`)}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err = first.PublishEvent(ctx, "ai", event.EventID, raw); err != nil {
		t.Fatal(err)
	}
	select {
	case <-unavailable:
	case <-ctx.Done():
		t.Fatal("dead-letter database outage was not exercised")
	}
	first.Close()

	recovered := make(chan DeadLetter, 2)
	sink := func(_ context.Context, letter DeadLetter) error { recovered <- letter; return nil }
	second := New(ctx, cfg)
	defer second.Close()
	second.SetDeadLetterSink(sink)
	third := New(ctx, cfg)
	defer third.Close()
	third.SetDeadLetterSink(sink)
	for _, client := range []*Client{second, third} {
		if err := client.SubscribeTask("ai", handler); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case letter := <-recovered:
		if letter.EventID != event.EventID || letter.EventType != event.EventType || letter.TaskCode != "ai" ||
			letter.Attempts != 1 || letter.AggregateType != event.AggregateType || letter.AggregateID != event.AggregateID ||
			string(letter.Payload) != string(raw) || letter.LastError != "synthetic provider rejected task" {
			t.Fatalf("recovered dead letter lost its identity, source envelope, or failure metadata: %+v", letter)
		}
	case <-ctx.Done():
		t.Fatal("restarted workers did not recover the broker dead letter")
	}
	consumer := cleanDurable(cfg.SubjectPrefix + "-dead-letter-recovery")
	for {
		info, err := second.jetStream.ConsumerInfo(stream, consumer)
		if err != nil {
			t.Fatal(err)
		}
		if info.NumAckPending == 0 && info.NumPending == 0 {
			break
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("database persistence was not acknowledged")
		}
	}
	if handlerCalls.Load() != 1 {
		t.Fatalf("database recovery repeated the task handler %d times", handlerCalls.Load())
	}
	select {
	case extra := <-recovered:
		t.Fatalf("competing healthy workers persisted the same broker delivery twice: %+v", extra)
	default:
	}
}

func TestDeadLetterSinkPreservesAdministratorReplayAndMalformedInputIntegration(t *testing.T) {
	pool := openOutboxStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sink := NewPostgresDeadLetterSink(pool)
	letter := DeadLetter{EventID: "synthetic-terminal", EventType: "synthetic.failed", TaskCode: "ai", FailureStage: "consumer:ai",
		AggregateType: "synthetic", AggregateID: "1", Payload: []byte(`{"synthetic":true}`), Attempts: 3, LastError: "synthetic failure"}
	if err := sink(ctx, letter); err != nil {
		t.Fatal(err)
	}
	var firstFailedAt time.Time
	if err := pool.QueryRow(ctx, `update dead_letter_events set replayed_at=now() where event_id=$1 returning failed_at`, letter.EventID).Scan(&firstFailedAt); err != nil {
		t.Fatal(err)
	}
	if err := sink(ctx, letter); err != nil {
		t.Fatal(err)
	}
	var replayedAt *time.Time
	var failedAt time.Time
	if err := pool.QueryRow(ctx, `select failed_at,replayed_at from dead_letter_events where event_id=$1`, letter.EventID).Scan(&failedAt, &replayedAt); err != nil {
		t.Fatal(err)
	}
	if replayedAt == nil || !failedAt.Equal(firstFailedAt) {
		t.Fatal("late broker acknowledgment recovery reopened an administrator-replayed event")
	}
	letter.EventID = "synthetic-malformed"
	letter.Payload = []byte("{not JSON}")
	if err := sink(ctx, letter); err != nil {
		t.Fatal(err)
	}
	var preserved string
	if err := pool.QueryRow(ctx, `select payload->>'invalidEnvelope' from dead_letter_events where event_id=$1`, letter.EventID).Scan(&preserved); err != nil {
		t.Fatal(err)
	}
	if preserved != string(letter.Payload) {
		t.Fatal("malformed event body was not preserved for inspection")
	}
}

func TestUnsupportedDeadLetterDoesNotStarveRecoveryIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	stream := uniqueJetStreamName("OCT02_DEAD_LEGACY_")
	cfg := config.NATSConfig{Enabled: true, URL: jetStreamIntegrationURL(t), SubjectPrefix: "dead-legacy-" + randomEventID(),
		JetStream: config.JetStreamConfig{Enabled: true, Stream: stream, MaxDeliver: 1, AckWait: time.Second, PublishTimeout: time.Second}}
	client := New(ctx, cfg)
	defer client.Close()
	client.mu.RLock()
	js := client.jetStream
	client.mu.RUnlock()
	if js == nil {
		t.Fatal("owned JetStream server unavailable")
	}
	t.Cleanup(func() { _ = js.DeleteStream(stream) })
	recovered := make(chan DeadLetter, 1)
	client.SetDeadLetterSink(func(_ context.Context, letter DeadLetter) error {
		recovered <- letter
		return nil
	})
	legacyBody := []byte(`{"legacy":true}`)
	legacyAck, err := js.Publish(fullSubject(cfg.SubjectPrefix, "dead-letter.legacy"), legacyBody)
	if err != nil {
		t.Fatal(err)
	}
	letter := DeadLetter{EventID: "synthetic-after-legacy", TaskCode: "test", Attempts: 1,
		Payload: []byte(`{"synthetic":true}`)}
	if err := client.publishDeadLetter(js, cfg, letter); err != nil {
		t.Fatal(err)
	}
	select {
	case actual := <-recovered:
		if actual.EventID != letter.EventID {
			t.Fatal("unsupported legacy message reached the database sink")
		}
	case <-ctx.Done():
		t.Fatal("unsupported legacy message blocked the valid recovery record")
	}
	legacy, err := js.GetMsg(stream, legacyAck.Sequence)
	if err != nil || string(legacy.Data) != string(legacyBody) {
		t.Fatal("terminating an unsupported delivery removed its broker inspection record")
	}
}
