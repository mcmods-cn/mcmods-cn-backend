package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"

	"github.com/nats-io/nats.go"
	"mcmods-cn-backend/internal/config"
)

func TestOPS007InitialFailureRecoversSubscriptionsWithoutReconfigure(t *testing.T) {
	harness := newRestartableJetStreamServer(t)
	harness.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := reconfigureTestConfig(harness.url())
	cfg.SubjectPrefix = "ops007-" + randomEventID()[:16]
	cfg.JetStream.Enabled = true
	cfg.JetStream.Stream = uniqueJetStreamName("OPS007_")
	client := New(ctx, cfg)
	defer client.Close()
	if status := client.Status(); status.Connected || status.LastError == "" {
		t.Fatalf("startup failure must be observable: %#v", status)
	}
	broadcasts := make(chan string, 8)
	tasks := make(chan struct{}, 4)
	if err := client.SubscribeBroadcast("user.*", func(_ context.Context, _ string, raw []byte) { broadcasts <- string(raw) }); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("offline subscription error = %v", err)
	}
	if err := client.SubscribeTask("ai", func(context.Context, []byte) error { tasks <- struct{}{}; return nil }); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("offline task registration error = %v", err)
	}
	harness.start(t)
	waitForNATSStatus(t, client, true)
	publisher := New(ctx, cfg)
	defer publisher.Close()
	assertOPS007Broadcast(t, ctx, publisher, broadcasts, "initial-recovery")
	publishEnvelope(t, ctx, publisher, "ai", "startup-recovery", `{"ok":true}`)
	select {
	case <-tasks:
	case <-time.After(3 * time.Second):
		t.Fatal("registered durable task did not recover after startup failure")
	}
	harness.stop()
	waitForNATSStatus(t, client, false)
	harness.start(t)
	waitForNATSStatus(t, client, true)
	waitForNATSStatus(t, publisher, true)
	assertOPS007Broadcast(t, ctx, publisher, broadcasts, "after-restart")
	publishEnvelope(t, ctx, publisher, "ai", "reconnect-recovery", `{"ok":true}`)
	select {
	case <-tasks:
	case <-time.After(3 * time.Second):
		t.Fatal("durable task did not resume after broker restart")
	}
	select {
	case payload := <-broadcasts:
		t.Fatalf("duplicate broadcast after recovery: %q", payload)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestOPS007ClosedClientCannotReconnectOrReconfigure(t *testing.T) {
	var zero Client
	zero.Close()
	zero.Close()
	harness := newRestartableJetStreamServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := reconfigureTestConfig(harness.url())
	client := New(ctx, cfg)
	if err := client.SubscribeTask("ai", func(context.Context, []byte) error { t.Error("closed local task executed"); return nil }); err != nil {
		t.Fatal(err)
	}
	client.Close()
	client.Close()
	if err := client.Reconfigure(cfg); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed client reconfiguration error = %v", err)
	}
	if client.Status().Connected {
		t.Fatal("closed client was resurrected")
	}
	if err := client.HandleLocally(ctx, "ai", "closed", nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed local task dispatch error = %v", err)
	}
}

func TestOPS007RecoveryUsesLatestDisabledConfiguration(t *testing.T) {
	harness := newRestartableJetStreamServer(t)
	harness.stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := reconfigureTestConfig(harness.url())
	client := New(ctx, cfg)
	defer client.Close()
	cfg.Enabled = false
	cfg.Realtime = false
	cfg.SubjectPrefix = "ops007-disabled"
	if err := client.Reconfigure(cfg); err != nil {
		t.Fatal(err)
	}
	harness.start(t)
	time.Sleep(2500 * time.Millisecond)
	if status := client.Status(); status.Enabled || status.Connected || status.SubjectPrefix != cfg.SubjectPrefix {
		t.Fatalf("recovery restored stale desired configuration: %#v", status)
	}
}

func TestOPS007ParentCancellationStopsInitialRecovery(t *testing.T) {
	harness := newRestartableJetStreamServer(t)
	harness.stop()
	ctx, cancel := context.WithCancel(context.Background())
	client := New(ctx, reconfigureTestConfig(harness.url()))
	defer client.Close()
	cancel()
	select {
	case <-client.done:
	case <-time.After(3 * time.Second):
		t.Fatal("parent cancellation did not close the recovery lifecycle")
	}
	harness.start(t)
	time.Sleep(2500 * time.Millisecond)
	if status := client.Status(); status.Connected || status.Recovering || harness.server.NumClients() != 0 {
		t.Fatalf("canceled client resumed recovery: status=%#v clients=%d", status, harness.server.NumClients())
	}
}

func TestOPS007SubscriptionDenialIsNotReportedReadyAndRecovers(t *testing.T) {
	harness := newRestartableJetStreamServer(t)
	harness.stop()
	harness.options.Users = []*natsserver.User{{Username: "ops007", Password: "test-only", Permissions: &natsserver.Permissions{
		Subscribe: &natsserver.SubjectPermission{Deny: []string{"mcmods.realtime.>"}},
	}}}
	harness.start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg := reconfigureTestConfig(harness.url())
	cfg.Username, cfg.Password = "ops007", "test-only"
	client := New(ctx, cfg)
	defer client.Close()
	messages := make(chan string, 8)
	err := client.SubscribeBroadcast("user.*", func(_ context.Context, _ string, raw []byte) { messages <- string(raw) })
	if !errors.Is(err, nats.ErrPermissionViolation) {
		t.Fatalf("broker subscription denial must be observable, got %v", err)
	}
	if status := client.Status(); !status.Connected || status.RealtimeReady || !status.Recovering || status.LastError == "" {
		t.Fatalf("denied subscription health = %#v", status)
	}
	harness.stop()
	harness.options.Users[0].Permissions = nil
	harness.start(t)
	deadline := time.Now().Add(10 * time.Second)
	for !client.Status().RealtimeReady && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if !client.Status().RealtimeReady {
		t.Fatalf("denied registration was not recovered: %#v", client.Status())
	}
	denied := newRestartableJetStreamServer(t)
	denied.stop()
	denied.options.Users = []*natsserver.User{{Username: "ops007", Password: "test-only", Permissions: &natsserver.Permissions{
		Subscribe: &natsserver.SubjectPermission{Deny: []string{"mcmods.realtime.>"}},
	}}}
	denied.start(t)
	candidate := cfg
	candidate.URL = denied.url()
	persistCalled := false
	if err = client.ReconfigureWithPersistence(candidate, func(config.NATSConfig) error { persistCalled = true; return nil }); !errors.Is(err, nats.ErrPermissionViolation) {
		t.Fatalf("denied runtime candidate error = %v", err)
	}
	if status := client.Status(); persistCalled || status.URL != cfg.URL || !status.RealtimeReady {
		t.Fatalf("denied candidate replaced last good runtime: persisted=%t status=%#v", persistCalled, status)
	}
	assertOPS007Broadcast(t, ctx, client, messages, "permission-restored")
	select {
	case payload := <-messages:
		t.Fatalf("duplicate subscription after denial recovery: %q", payload)
	case <-time.After(150 * time.Millisecond):
	}
}

func assertOPS007Broadcast(t *testing.T, ctx context.Context, publisher *Client, messages <-chan string, payload string) {
	t.Helper()
	if err := publisher.PublishBroadcast(ctx, "user.42", payload); err != nil {
		t.Fatal(err)
	}
	select {
	case actual := <-messages:
		if actual != `"`+payload+`"` {
			t.Fatalf("broadcast = %q, want %q", actual, payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("broadcast %q was not delivered", payload)
	}
}
