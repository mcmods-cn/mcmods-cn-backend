package queue

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
)

// jetStreamIntegrationURL makes the durable queue tests part of the default
// suite. A deployment may still opt into an externally managed server, but a
// developer or CI runner never needs Docker or a preinstalled nats-server.
func jetStreamIntegrationURL(t *testing.T) string {
	t.Helper()
	if os.Getenv("MCMODS_RUN_NATS_INTEGRATION") == "1" {
		if external := strings.TrimSpace(os.Getenv("MCMODS_TEST_NATS_URL")); external != "" {
			return external
		}
	}
	options := &natsserver.Options{
		ServerName: "mcmods-jetstream-test",
		Host:       "127.0.0.1",
		Port:       -1,
		JetStream:  true,
		StoreDir:   t.TempDir(),
		NoLog:      true,
		NoSigs:     true,
	}
	server, err := natsserver.NewServer(options)
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	if !server.ReadyForConnections(10 * time.Second) {
		server.Shutdown()
		t.Fatal("embedded NATS server did not become ready")
	}
	t.Cleanup(func() {
		server.Shutdown()
		server.WaitForShutdown()
	})
	return server.ClientURL()
}

func uniqueJetStreamName(prefix string) string {
	return prefix + strings.ToUpper(randomEventID()[:16])
}

type restartableJetStreamServer struct {
	mu      sync.Mutex
	options natsserver.Options
	server  *natsserver.Server
}

func newRestartableJetStreamServer(t *testing.T) *restartableJetStreamServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	harness := &restartableJetStreamServer{options: natsserver.Options{
		ServerName: "mcmods-restart-test", Host: "127.0.0.1", Port: port,
		JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true,
	}}
	harness.start(t)
	t.Cleanup(harness.stop)
	return harness
}

func (harness *restartableJetStreamServer) url() string {
	return fmt.Sprintf("nats://%s:%d", harness.options.Host, harness.options.Port)
}

func (harness *restartableJetStreamServer) start(t *testing.T) {
	t.Helper()
	harness.mu.Lock()
	defer harness.mu.Unlock()
	if harness.server != nil {
		t.Fatal("embedded NATS server is already running")
	}
	options := harness.options
	server, err := natsserver.NewServer(&options)
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	if !server.ReadyForConnections(10 * time.Second) {
		server.Shutdown()
		t.Fatal("restartable NATS server did not become ready")
	}
	harness.server = server
}

func (harness *restartableJetStreamServer) stop() {
	harness.mu.Lock()
	server := harness.server
	harness.server = nil
	harness.mu.Unlock()
	if server != nil {
		server.Shutdown()
		server.WaitForShutdown()
	}
}
