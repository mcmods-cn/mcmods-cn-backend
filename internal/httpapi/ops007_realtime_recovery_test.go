package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

func TestOPS007RealtimeHubRecoversInitialFailureAndBrokerRestart(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	var broker *natsserver.Server
	startBroker := func() {
		var startErr error
		broker, startErr = natsserver.NewServer(&natsserver.Options{Host: "127.0.0.1", Port: port, NoLog: true, NoSigs: true})
		if startErr != nil {
			t.Fatal(startErr)
		}
		go broker.Start()
		if !broker.ReadyForConnections(5 * time.Second) {
			t.Fatal("OPS007 broker did not become ready")
		}
	}
	stopBroker := func() {
		if broker != nil {
			broker.Shutdown()
			broker.WaitForShutdown()
			broker = nil
		}
	}
	defer stopBroker()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := config.NATSConfig{Enabled: true, URL: fmt.Sprintf("nats://127.0.0.1:%d", port), SubjectPrefix: "ops007", Realtime: true}
	firstQueue, secondQueue := queue.New(ctx, cfg), queue.New(ctx, cfg)
	defer firstQueue.Close()
	defer secondQueue.Close()
	first := &Server{queue: firstQueue, realtime: newRealtimeHub()}
	second := &Server{queue: secondQueue, realtime: newRealtimeHub()}
	first.subscribeRealtimeBroadcast()
	second.subscribeRealtimeBroadcast()
	firstEvents, unsubscribeFirst := first.realtime.subscribe(42)
	defer unsubscribeFirst()
	secondEvents, unsubscribeSecond := second.realtime.subscribe(42)
	defer unsubscribeSecond()
	assertOPS007Health(t, first, "degraded")
	first.publishRealtimeUser(42, "notification.offline", nil)
	receiveBUG046RealtimeEvent(t, firstEvents, "offline local Hub")
	assertNoBUG046RealtimeEvent(t, secondEvents, "offline broadcast unexpectedly persisted")
	startBroker()
	waitOPS007RealtimeHealth(t, first, true)
	waitOPS007RealtimeHealth(t, second, true)
	assertOPS007Health(t, first, "ready")
	first.publishRealtimeUser(42, "notification.recovered", nil)
	firstEvent := receiveBUG046RealtimeEvent(t, firstEvents, "recovered origin")
	secondEvent := receiveBUG046RealtimeEvent(t, secondEvents, "recovered peer")
	if firstEvent.ID == "" || firstEvent.ID != secondEvent.ID || firstEvent.Type != secondEvent.Type {
		t.Fatalf("recovered event identity diverged: %#v / %#v", firstEvent, secondEvent)
	}
	assertNoBUG046RealtimeEvent(t, firstEvents, "origin received its recovery echo")
	assertNoBUG046RealtimeEvent(t, secondEvents, "peer received a recovery duplicate")
	stopBroker()
	waitOPS007RealtimeHealth(t, first, false)
	waitOPS007RealtimeHealth(t, second, false)
	assertOPS007Health(t, second, "degraded")
	startBroker()
	waitOPS007RealtimeHealth(t, first, true)
	waitOPS007RealtimeHealth(t, second, true)
	second.publishRealtimeUser(42, "notification.restarted", nil)
	firstEvent = receiveBUG046RealtimeEvent(t, firstEvents, "restarted peer")
	secondEvent = receiveBUG046RealtimeEvent(t, secondEvents, "restarted origin")
	if firstEvent.ID == "" || firstEvent.ID != secondEvent.ID || firstEvent.Type != "notification.restarted" || secondEvent.Type != firstEvent.Type {
		t.Fatalf("restarted event identity diverged: %#v / %#v", firstEvent, secondEvent)
	}
	assertNoBUG046RealtimeEvent(t, firstEvents, "peer received a reconnect duplicate")
	assertNoBUG046RealtimeEvent(t, secondEvents, "origin received its reconnect echo")
	firstQueue.Close()
	assertOPS007Health(t, first, "degraded")
	if first.realtimeHealth(firstQueue.Status()).BroadcastRecovering {
		t.Fatal("terminally closed client must not claim ongoing recovery")
	}
}

func waitOPS007RealtimeHealth(t *testing.T, server *Server, ready bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if server.queue.Status().RealtimeReady == ready {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("broadcast readiness did not become %t: %#v", ready, server.queue.Status())
}

func assertOPS007Health(t *testing.T, server *Server, broadcast string) {
	t.Helper()
	response := httptest.NewRecorder()
	server.ready(response, httptest.NewRequest(http.MethodGet, "/ready", nil))
	var body struct {
		Data struct {
			Dependencies map[string]string `json:"dependencies"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// PostgreSQL is deliberately absent: optional realtime readiness remains
	// observable independently, without weakening the database readiness gate.
	if response.Code != http.StatusServiceUnavailable || body.Data.Dependencies["postgresql"] != "unavailable" ||
		body.Data.Dependencies["realtimeLocal"] != "ready" || body.Data.Dependencies["realtimeBroadcast"] != broadcast {
		t.Fatalf("readiness status/body = %d / %s", response.Code, response.Body.String())
	}
}
