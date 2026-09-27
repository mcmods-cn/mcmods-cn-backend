package queue

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestReconfigureStagesAllSubscriptionsAndKeepsLastGoodRuntime(t *testing.T) {
	oldServer := newProtocolTestNATSServer(t, false)
	deniedServer := newProtocolTestNATSServer(t, true)
	nextServer := newProtocolTestNATSServer(t, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := New(ctx, reconfigureTestConfig(oldServer.url))
	defer client.Close()
	if status := client.Status(); !status.Connected {
		t.Fatalf("initial test NATS connection failed: %#v", status)
	}
	if err := client.SubscribeTask("ai", func(context.Context, []byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := client.SubscribeBroadcast("user.*", func(context.Context, string, []byte) {}); err != nil {
		t.Fatal(err)
	}
	oldServer.waitForSubscriptions(t, 2)

	persistCalled := false
	err := client.ReconfigureWithPersistence(reconfigureTestConfig(deniedServer.url), func(config.NATSConfig) error {
		persistCalled = true
		return nil
	})
	if err == nil {
		t.Fatal("expected candidate subscription failure")
	}
	if persistCalled {
		t.Fatal("configuration was persisted before all candidate subscriptions succeeded")
	}
	assertActiveNATSURL(t, client, oldServer.url)

	persistFailure := errors.New("injected persistence failure")
	err = client.ReconfigureWithPersistence(reconfigureTestConfig(nextServer.url), func(config.NATSConfig) error {
		assertActiveNATSURL(t, client, oldServer.url)
		return persistFailure
	})
	if !errors.Is(err, persistFailure) {
		t.Fatalf("expected persistence failure, got %v", err)
	}
	nextServer.waitForSubscriptions(t, 2)
	nextServer.waitForConnections(t, 0)
	assertActiveNATSURL(t, client, oldServer.url)

	if err = client.ReconfigureWithPersistence(reconfigureTestConfig(nextServer.url), func(config.NATSConfig) error {
		assertActiveNATSURL(t, client, oldServer.url)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	nextServer.waitForSubscriptions(t, 4)
	assertActiveNATSURL(t, client, nextServer.url)
	oldServer.waitForConnections(t, 0)
}

func TestBroadcastSubscriptionRegisteredWhileRealtimeIsDisabled(t *testing.T) {
	server := newProtocolTestNATSServer(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := New(ctx, config.NATSConfig{Enabled: false, Realtime: false})
	defer client.Close()
	if err := client.SubscribeBroadcast("user.*", func(context.Context, string, []byte) {}); !errors.Is(err, ErrTaskDisabled) {
		t.Fatalf("expected disabled registration to report task disabled, got %v", err)
	}
	if err := client.Reconfigure(reconfigureTestConfig(server.url)); err != nil {
		t.Fatal(err)
	}
	server.waitForSubscriptions(t, 1)
}

func reconfigureTestConfig(url string) config.NATSConfig {
	return config.NATSConfig{
		Enabled: true, URL: url, SubjectPrefix: "mcmods", Realtime: true, OutboxEnabled: true,
		Tasks: []config.NATSTaskConfig{{
			Code: "ai", Enabled: true, Subject: "ai.tasks", QueueGroup: "test-ai", MaxConcurrent: 1, TimeoutSeconds: 30,
		}},
		JetStream: config.JetStreamConfig{Stream: "TEST", MaxDeliver: 3, AckWait: time.Second, PublishTimeout: time.Second},
	}
}

func assertActiveNATSURL(t *testing.T, client *Client, expected string) {
	t.Helper()
	status := client.Status()
	if !status.Connected || status.URL != expected {
		t.Fatalf("expected active NATS runtime %q, got %#v", expected, status)
	}
}

type protocolTestNATSServer struct {
	listener net.Listener
	url      string
	denySUB  bool

	mu            sync.Mutex
	connections   map[net.Conn]struct{}
	subscriptions int
}

func newProtocolTestNATSServer(t *testing.T, denySubscriptions bool) *protocolTestNATSServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &protocolTestNATSServer{listener: listener, url: "nats://" + listener.Addr().String(), denySUB: denySubscriptions, connections: make(map[net.Conn]struct{})}
	go server.accept()
	t.Cleanup(server.close)
	return server
}

func (server *protocolTestNATSServer) accept() {
	for {
		conn, err := server.listener.Accept()
		if err != nil {
			return
		}
		server.mu.Lock()
		server.connections[conn] = struct{}{}
		server.mu.Unlock()
		go server.serve(conn)
	}
}

func (server *protocolTestNATSServer) serve(conn net.Conn) {
	defer func() {
		_ = conn.Close()
		server.mu.Lock()
		delete(server.connections, conn)
		server.mu.Unlock()
	}()
	address := server.listener.Addr().(*net.TCPAddr)
	_, _ = fmt.Fprintf(conn, "INFO {\"server_id\":\"audit-test\",\"version\":\"2.10.0\",\"proto\":1,\"host\":\"127.0.0.1\",\"port\":%d,\"max_payload\":1048576}\r\n", address.Port)
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "PING":
			_, _ = io.WriteString(conn, "PONG\r\n")
		case "SUB":
			server.mu.Lock()
			server.subscriptions++
			server.mu.Unlock()
			if server.denySUB {
				_, _ = io.WriteString(conn, "-ERR 'Permissions Violation for Subscription'\r\n")
				return
			}
		case "PUB", "HPUB":
			length, parseErr := strconv.Atoi(fields[len(fields)-1])
			if parseErr != nil {
				return
			}
			if _, err = io.CopyN(io.Discard, reader, int64(length+2)); err != nil {
				return
			}
		}
	}
}

func (server *protocolTestNATSServer) waitForSubscriptions(t *testing.T, expected int) {
	t.Helper()
	server.waitFor(t, func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return server.subscriptions >= expected
	}, fmt.Sprintf("at least %d subscriptions", expected))
}

func (server *protocolTestNATSServer) waitForConnections(t *testing.T, expected int) {
	t.Helper()
	server.waitFor(t, func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return len(server.connections) == expected
	}, fmt.Sprintf("%d connections", expected))
}

func (server *protocolTestNATSServer) waitFor(t *testing.T, condition func() bool, description string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

func (server *protocolTestNATSServer) close() {
	_ = server.listener.Close()
	server.mu.Lock()
	connections := make([]net.Conn, 0, len(server.connections))
	for conn := range server.connections {
		connections = append(connections, conn)
	}
	server.mu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
}
