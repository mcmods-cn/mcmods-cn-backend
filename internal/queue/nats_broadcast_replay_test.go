package queue

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func TestOPS007BroadcastReadinessWaitsForBrokerReplay(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	brokerURL, subscriber, messages, releaseReplay := holdSubscriptionReplay(t, ctx)
	status := subscriber.Status()
	if !status.Connected {
		t.Fatalf("transport was not connected at the replay barrier: %#v", status)
	}
	t.Logf("SUB held before broker: connected=%t realtimeReady=%t recovering=%t", status.Connected, status.RealtimeReady, status.Recovering)
	if status.RealtimeReady || !status.Recovering {
		t.Errorf("realtime health must remain recovering while the broker has not received the replay: %#v", status)
	}

	publisher := New(ctx, reconfigureTestConfig(brokerURL))
	defer publisher.Close()
	if err := publisher.PublishBroadcast(ctx, "user.42", "before-replay"); err != nil {
		t.Fatal(err)
	}
	// Ensure the broker processed this Core NATS publish while there was no
	// subscription. It is not replayed as durable work when the route returns.
	if err := replayConnection(publisher).FlushWithContext(ctx); err != nil {
		t.Fatal(err)
	}
	releaseReplay()
	if err := replayConnection(subscriber).FlushWithContext(ctx); err != nil {
		t.Fatal(err)
	}
	waitForRealtimeBroadcastReady(t, subscriber)
	assertOPS007Broadcast(t, ctx, publisher, messages, "after-replay")
	select {
	case duplicate := <-messages:
		t.Fatalf("unexpected additional broadcast after replay: %q", duplicate)
	default:
	}
}

func TestOPS007UnconfirmedBroadcastReplayRestagesConnection(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	brokerURL, subscriber, messages, _ := holdSubscriptionReplay(t, ctx)
	unconfirmed := replayConnection(subscriber)

	// The test leaves the replay pending on the first resumed socket. The
	// bounded PONG wait must let the recovery loop stage a working replacement.
	waitForRealtimeBroadcastReady(t, subscriber)
	if replayConnection(subscriber) == unconfirmed {
		t.Fatal("a socket whose replay was never confirmed reported ready")
	}
	publisher := New(ctx, reconfigureTestConfig(brokerURL))
	defer publisher.Close()
	assertOPS007Broadcast(t, ctx, publisher, messages, "restaged")
}

func TestOPS007CancelWhileBroadcastReplayIsUnconfirmed(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, subscriber, _, releaseReplay := holdSubscriptionReplay(t, ctx)
	cancel()
	select {
	case <-subscriber.done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not close the client while replay confirmation was pending")
	}
	releaseReplay()
	if status := subscriber.Status(); status.Connected || status.RealtimeReady || status.Recovering {
		t.Fatalf("canceled queue reported live after releasing the replay: %#v", status)
	}
}

func TestOPS007BrokerDenialOfBroadcastReplayKeepsDegraded(t *testing.T) {
	t.Parallel()
	broker := newRestartableJetStreamServer(t)
	broker.stop()
	broker.options.Users = []*natsserver.User{{Username: "replay", Password: "test-only"}}
	broker.start(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cfg := reconfigureTestConfig(broker.url())
	cfg.Username, cfg.Password = "replay", "test-only"
	client := New(ctx, cfg)
	defer client.Close()
	messages := make(chan string, 2)
	if err := client.SubscribeBroadcast("user.*", func(_ context.Context, _ string, raw []byte) {
		messages <- string(raw)
	}); err != nil {
		t.Fatal(err)
	}
	broker.stop()
	waitForNATSStatus(t, client, false)
	broker.options.Users[0].Permissions = &natsserver.Permissions{
		Subscribe: &natsserver.SubjectPermission{Deny: []string{"mcmods.realtime.>"}},
	}
	broker.start(t)
	deadline := time.Now().Add(10 * time.Second)
	var denied Status
	for time.Now().Before(deadline) {
		denied = client.Status()
		if denied.Connected && strings.Contains(denied.LastError, "Subscription to") {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !denied.Connected || !strings.Contains(denied.LastError, "Subscription to") || denied.RealtimeReady || !denied.Recovering {
		t.Fatalf("ready state after replay rejection: %#v", denied)
	}
	broker.stop()
	waitForNATSStatus(t, client, false)
	broker.options.Users[0].Permissions = nil
	broker.start(t)
	waitForRealtimeBroadcastReady(t, client)
	assertOPS007Broadcast(t, ctx, client, messages, "allowed-again")
}

func holdSubscriptionReplay(t *testing.T, ctx context.Context) (string, *Client, <-chan string, func()) {
	t.Helper()
	broker := newRestartableJetStreamServer(t)
	url, pause, replayHeld, releaseReplay := newSubscriptionReplayProxy(t, ctx, broker.url(), "mcmods.realtime.user.*")
	subscriber := New(ctx, reconfigureTestConfig(url))
	t.Cleanup(subscriber.Close)
	messages := make(chan string, 2)
	if err := subscriber.SubscribeBroadcast("user.*", func(_ context.Context, _ string, raw []byte) {
		messages <- string(raw)
	}); err != nil {
		t.Fatal(err)
	}
	if status := subscriber.Status(); !status.RealtimeReady {
		t.Fatalf("initial subscription was not ready: %#v", status)
	}
	broker.stop()
	waitForNATSStatus(t, subscriber, false)
	pause()
	broker.start(t)
	select {
	case <-replayHeld:
	case <-ctx.Done():
		t.Fatal("reconnect did not replay the subscription")
	}
	return broker.url(), subscriber, messages, releaseReplay
}

func replayConnection(client *Client) *nats.Conn {
	client.mu.RLock()
	defer client.mu.RUnlock()
	return client.conn
}

func waitForRealtimeBroadcastReady(t *testing.T, client *Client) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if client.Status().RealtimeReady {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("realtime broadcast did not become ready: %#v", client.Status())
}

// The publisher connects directly to the real broker. This test-only relay
// holds the subscriber's replayed SUB after its new handshake and before the
// SUB reaches the broker. All other bytes are forwarded in order.
func newSubscriptionReplayProxy(t *testing.T, ctx context.Context, brokerURL, subject string) (string, func(), <-chan struct{}, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var pause atomic.Bool
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	done := make(chan struct{})
	go func() {
		defer close(done)
		var tunnels sync.WaitGroup
		defer tunnels.Wait()
		for {
			incoming, err := listener.Accept()
			if err != nil {
				return
			}
			tunnels.Go(func() {
				defer incoming.Close()
				upstream, err := (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(brokerURL, "nats://"))
				if err != nil {
					return
				}
				closeTunnel := func() {
					_ = incoming.Close()
					_ = upstream.Close()
				}
				stop := context.AfterFunc(ctx, closeTunnel)
				defer stop()
				forwarded := make(chan struct{})
				go func() {
					defer close(forwarded)
					defer closeTunnel()
					_, _ = io.Copy(incoming, upstream)
				}()
				defer func() {
					closeTunnel()
					<-forwarded
				}()
				reader := bufio.NewReader(incoming)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if strings.HasPrefix(line, "SUB "+subject+" ") && pause.CompareAndSwap(true, false) {
						close(held)
						select {
						case <-release:
						case <-ctx.Done():
							return
						}
					}
					if _, err := io.WriteString(upstream, line); err != nil {
						return
					}
				}
			})
		}
	}()
	t.Cleanup(func() {
		resume()
		_ = listener.Close()
		<-done
	})
	return "nats://" + listener.Addr().String(), func() { pause.Store(true) }, held, resume
}
