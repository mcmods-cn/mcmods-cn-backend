package queue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"mcmods-cn-backend/internal/config"
)

func deliveryTestEnvelope(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(EventEnvelope{EventID: "owned-test", EventType: "test.delivery", SchemaVersion: 1,
		OccurredAt: time.Now().UTC(), AggregateType: "test", AggregateID: "owned", Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestLocalDeliveryHonorsTaskPolicyAndQueueLifetime(t *testing.T) {
	raw := deliveryTestEnvelope(t)
	cfg := config.NATSConfig{Tasks: []config.NATSTaskConfig{{Code: "ai", Enabled: false, TimeoutSeconds: 1}}}
	client := New(context.Background(), cfg)
	defer client.Close()
	deadline := make(chan time.Duration, 1)
	handler := func(ctx context.Context, _ []byte) error {
		until, ok := ctx.Deadline()
		if !ok {
			return errors.New("local handler missing timeout")
		}
		deadline <- time.Until(until)
		<-ctx.Done()
		return ctx.Err()
	}
	_ = client.SubscribeTask("ai", handler)
	if err := client.HandleLocally(context.Background(), "ai", "owned-test", raw); !errors.Is(err, ErrTaskDisabled) {
		t.Fatalf("disabled local task returned %v", err)
	}
	cfg.Tasks[0].Enabled = true
	if err := client.Reconfigure(cfg); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- client.HandleLocally(context.Background(), "ai", "owned-test", raw) }()
	select {
	case d := <-deadline:
		if d <= 0 || d > time.Second {
			t.Fatalf("local deadline exceeded task timeout: %s", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("enabled task did not get its configured deadline")
	}
	client.Close()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("client close did not stop local work: %v", err)
	}
}

func TestDurablePublishRequiresBrokerConfirmation(t *testing.T) {
	client := New(context.Background(), reconfigureTestConfig(jetStreamIntegrationURL(t)))
	defer client.Close()
	if err := client.PublishEvent(context.Background(), "ai", "owned-test", deliveryTestEnvelope(t)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("publish without a JetStream acknowledgment returned %v", err)
	}
}

func TestJetStreamLongHandlerAndRetryDelayIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const ackWait = 300 * time.Millisecond
	cfg := config.NATSConfig{Enabled: true, URL: jetStreamIntegrationURL(t), SubjectPrefix: "delivery-" + randomEventID(),
		JetStream: config.JetStreamConfig{Enabled: true, Stream: uniqueJetStreamName("DELIVERY_"), AckWait: ackWait, MaxDeliver: 4},
		Tasks:     []config.NATSTaskConfig{{Code: "test", Enabled: true, Subject: "test", QueueGroup: "workers", MaxConcurrent: 2, TimeoutSeconds: 5}}}
	first, second := New(ctx, cfg), New(ctx, cfg)
	defer first.Close()
	defer second.Close()
	js := first.jetStream
	if js == nil {
		t.Fatal("owned JetStream unavailable")
	}
	defer js.DeleteStream(cfg.JetStream.Stream)
	var attempts atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	failed, retried := make(chan time.Time, 1), make(chan time.Time, 1)
	handle := func(ctx context.Context, _ []byte) error {
		switch attempts.Add(1) {
		case 1:
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			failed <- time.Now()
			return errors.New("synthetic first attempt")
		case 2:
			retried <- time.Now()
		default:
			return errors.New("unexpected extra delivery")
		}
		return nil
	}
	for _, client := range []*Client{first, second} {
		if err := client.SubscribeTask("test", handle); err != nil {
			t.Fatal(err)
		}
	}
	publishEnvelope(t, ctx, first, "test", "long", `{}`)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	time.Sleep(3 * ackWait)
	info, err := js.ConsumerInfo(cfg.JetStream.Stream, "workers", nats.Context(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if info.Delivered.Consumer != 1 || attempts.Load() != 1 {
		t.Fatalf("broker delivered %d and handlers started %d before the first attempt finished: %v", info.Delivered.Consumer, attempts.Load(), err)
	}
	releaseOnce.Do(func() { close(release) })
	failureTime := <-failed
	select {
	case retryTime := <-retried:
		if elapsed := retryTime.Sub(failureTime); elapsed < ackWait*9/10 {
			t.Fatalf("handler error retried after %s, before configured delay %s", elapsed, ackWait)
		}
	case <-ctx.Done():
		t.Fatal("handler error was not retried")
	}
}

func TestCandidateRollbackRetainsPeerAcknowledgedMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := New(ctx, config.NATSConfig{})
	defer client.Close()
	cfg := config.NATSConfig{Enabled: true, URL: jetStreamIntegrationURL(t), SubjectPrefix: "peer-" + randomEventID(),
		JetStream: config.JetStreamConfig{Enabled: true, Stream: uniqueJetStreamName("PEER_")}}
	peer, err := nats.Connect(cfg.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	js, err := peer.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	defer js.DeleteStream(cfg.JetStream.Stream)
	rejected := errors.New("synthetic settings persistence failure")
	var ack *nats.PubAck
	err = client.ReconfigureWithPersistence(cfg, func(config.NATSConfig) error {
		ack, err = js.Publish(cfg.SubjectPrefix+".peer", []byte("owned fixture"), nats.Context(ctx))
		if err != nil {
			return err
		}
		return rejected
	})
	if !errors.Is(err, rejected) || client.Status().Connected {
		t.Fatalf("candidate replaced active settings: %v", err)
	}
	got, err := js.GetMsg(ack.Stream, ack.Sequence, nats.Context(ctx))
	if err != nil || string(got.Data) != "owned fixture" {
		t.Fatalf("candidate rollback lost an acknowledged peer write: %v", err)
	}
}

func TestRecoverySharesTheLimitAndPauseWithLocalDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg := config.NATSConfig{Tasks: []config.NATSTaskConfig{{Code: "notifications", Enabled: true, TimeoutSeconds: 3, MaxConcurrent: 1}}}
	client := New(ctx, cfg)
	defer client.Close()
	started, release := make(chan struct{}), make(chan struct{})
	recovery := make(chan error, 1)
	go func() {
		recovery <- client.RunTask(ctx, "notifications", func(work context.Context) error {
			close(started)
			select {
			case <-release:
				return nil
			case <-work.Done():
				return work.Err()
			}
		})
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	called := make(chan struct{}, 1)
	_ = client.SubscribeTask("notifications", func(ctx context.Context, _ []byte) error { called <- struct{}{}; return nil })
	raw := deliveryTestEnvelope(t)
	result := make(chan error, 1)
	go func() { result <- client.HandleLocally(ctx, "notifications", "owned-test", raw) }()
	select {
	case <-called:
		t.Fatal("outbox exceeded the task concurrency budget held by the recovery worker")
	case <-time.After(80 * time.Millisecond):
	}
	cfg.Tasks[0].Enabled = false
	if err := client.Reconfigure(cfg); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrTaskDisabled) {
			t.Fatalf("waiting operation escaped pause: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("a policy change did not wake the waiter")
	}
	close(release)
	if err := <-recovery; err != nil {
		t.Fatal(err)
	}
	if got := client.TaskRuns()["notifications"]; got.Finished != 1 || got.Active != 0 || got.Failed != 0 {
		t.Fatalf("unexpected per-instance execution metrics: %+v", got)
	}
}

func TestPreprovisionedStreamRetainsItsLimitsAndReportsUsage(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	url, stream, prefix := jetStreamIntegrationURL(t), uniqueJetStreamName("LIMIT_"), "limit-"+randomEventID()
	operator, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer operator.Close()
	js, err := operator.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	_, err = js.AddStream(&nats.StreamConfig{Name: stream, Subjects: []string{prefix + ".>"}, Storage: nats.FileStorage,
		Retention: nats.LimitsPolicy, Discard: nats.DiscardNew, MaxMsgs: 1, MaxBytes: 32768})
	if err != nil {
		t.Fatal(err)
	}
	defer js.DeleteStream(stream)
	client := New(ctx, config.NATSConfig{Enabled: true, URL: url, SubjectPrefix: prefix, JetStream: config.JetStreamConfig{Enabled: true, Stream: stream}})
	defer client.Close()
	first, err := js.Publish(prefix+".fixture", []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js.Publish(prefix+".fixture", []byte("second")); err == nil {
		t.Fatal("overflow publish unexpectedly acknowledged")
	}
	got, err := js.GetMsg(stream, first.Sequence)
	if err != nil || string(got.Data) != "first" {
		t.Fatalf("existing backlog did not survive overflow: %v", err)
	}
	sample := client.StreamUsage(ctx)
	if sample == nil || sample.Messages != 1 || sample.MaxBytes != 32768 || sample.MaxMessages != 1 || sample.Discard != nats.DiscardNew.String() || sample.Bytes == 0 {
		t.Fatalf("stream usage does not reflect broker state: %+v", sample)
	}
}

func TestCoreOnlyPeerConsumesTasksExclusivelyFromPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg := reconfigureTestConfig(jetStreamIntegrationURL(t))
	cfg.SubjectPrefix = "mixed-" + randomEventID()
	local := New(ctx, cfg)
	defer local.Close()
	localCalls := make(chan string, 2)
	if err := local.SubscribeTask("ai", func(ctx context.Context, _ []byte) error {
		localCalls <- EventIDFromContext(ctx)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg.JetStream.Enabled, cfg.JetStream.Stream = true, uniqueJetStreamName("MIXED_")
	durable := New(ctx, cfg)
	defer durable.Close()
	if durable.jetStream == nil {
		t.Fatal("owned JetStream unavailable")
	}
	defer durable.jetStream.DeleteStream(cfg.JetStream.Stream)
	received := make(chan string, 1)
	if err := durable.SubscribeTask("ai", func(ctx context.Context, _ []byte) error {
		received <- EventIDFromContext(ctx)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	publishEnvelope(t, ctx, durable, "ai", "broker-event", `{}`)
	select {
	case got := <-received:
		if !strings.HasPrefix(got, "broker-event-") {
			t.Fatal(got)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// The registered local operation must also remain usable for a database claim.
	if err := local.HandleLocally(ctx, "ai", "owned-test", deliveryTestEnvelope(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-localCalls:
		if got != "owned-test" {
			t.Fatalf("Core-only peer also executed a JetStream task: %s", got)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case got := <-localCalls:
		t.Fatalf("unexpected second local execution: %s", got)
	case <-time.After(250 * time.Millisecond):
	}
}
