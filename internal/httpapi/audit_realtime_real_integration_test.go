package httpapi

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"mcmods-cn-backend/internal/queue"
)

func startAuditRealRedis(t *testing.T, port int) func() {
	t.Helper()
	binary := os.Getenv("MCMODS_TEST_REDIS_BINARY")
	if binary == "" {
		t.Skip("set MCMODS_TEST_REDIS_BINARY to an owned real redis-server executable")
	}
	cmd := exec.Command(binary, "--bind", "127.0.0.1", "--port", strconv.Itoa(port), "--save", "", "--appendonly", "no", "--dir", t.TempDir(), "--loglevel", "warning")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() { once.Do(func() { _ = cmd.Process.Signal(syscall.SIGTERM); _ = cmd.Wait() }) }
	t.Cleanup(stop)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return stop
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	t.Fatal("owned Redis did not listen")
	return nil
}

func TestAuditMSG001And003RealRedisTwoHTTPInstancesColdRestart(t *testing.T) {
	if os.Getenv("MCMODS_TEST_REDIS_BINARY") == "" {
		t.Skip("requires task-owned real Redis executable")
	}
	f := newTEST013Fixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	stopRedis := startAuditRealRedis(t, port)
	cfg := f.origin.Config.Handler.(*Server).cfg
	cfg.Redis.Enabled = true
	cfg.Redis.Required = true
	cfg.Redis.RateLimitFailClosed = true
	cfg.ReplicaCount = 2
	cfg.Redis.Addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	cfg.Redis.Namespace = "audit-" + randomCatalogPublicID()
	cfg.Redis.AuthSessionCacheEnabled = false
	cfg.Redis.RBACCacheEnabled = false
	cfg.Redis.DialTimeout = 50 * time.Millisecond
	cfg.Redis.ReadTimeout = 50 * time.Millisecond
	cfg.Redis.WriteTimeout = 50 * time.Millisecond
	origins := make([]*httptest.Server, 0, 2)
	for range 2 {
		q := queue.New(f.ctx, cfg.NATS)
		t.Cleanup(q.Close)
		s := NewServer(f.ctx, cfg, f.db, q, nil, nil, nil)
		t.Cleanup(func() { s.Shutdown(context.Background()); s.cache.Close() })
		origin := httptest.NewServer(s)
		t.Cleanup(origin.Close)
		origins = append(origins, origin)
	}
	request := func(index int, path string, want int) *http.Response {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, origins[index].URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+f.editor)
		response, err := origins[index].Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			defer response.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
			t.Fatalf("%s=%d want%d: %s", path, response.StatusCode, want, body)
		}
		return response
	}
	path := "/api/v1/realtime/events"
	first := request(0, path, 200)
	defer first.Body.Close()
	second := request(1, path, 200)
	defer second.Body.Close()
	request(0, path, 429).Body.Close()
	request(0, "/ready", 200).Body.Close()
	stopRedis()
	request(0, "/ready", 503).Body.Close()
	request(1, "/ready", 503).Body.Close()
	request(0, path, 429).Body.Close()
	request(1, path, 429).Body.Close()
	request(0, "/live", 200).Body.Close()
	// Cold Redis has lost every shared key. PostgreSQL must still reject a
	// third connection while the two established HTTP connections are alive.
	stopRedis = startAuditRealRedis(t, port)
	recoveryDeadline := time.Now().Add(5 * time.Second)
	for {
		response, err := origins[0].Client().Get(origins[0].URL + "/ready")
		if err != nil {
			t.Fatal(err)
		}
		status := response.StatusCode
		response.Body.Close()
		if status == 200 {
			break
		}
		if status != 503 || time.Now().After(recoveryDeadline) {
			t.Fatalf("readiness did not recover after owned Redis restart: %d", status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var established int
	if err = f.db.QueryRow(f.ctx, `select count(*) from realtime_connection_leases`).Scan(&established); err != nil || established != 2 {
		t.Fatalf("cold-restart barrier lost live leases: %d err=%v", established, err)
	}
	request(0, path, 429).Body.Close()
	request(1, path, 429).Body.Close()
	first.Body.Close()
	second.Body.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var count int
		if err = f.db.QueryRow(f.ctx, `select count(*) from realtime_connection_leases`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("released SSE leaked %d durable leases", count)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Rejected post-restart Redis leases have been released by their handlers.
	replacement := request(1, path, 200)
	replacement.Body.Close()
	request(1, "/ready", 200).Body.Close()
	stopRedis()
}

func TestAuditMSG003OptionalRedisKeepsIndependentReadinessSemantics(t *testing.T) {
	f := newTEST022Fixture(t, true)
	if f.servers[0].cfg.Redis.Required || f.servers[0].cfg.Redis.RateLimitFailClosed || f.servers[0].cfg.ReplicaCount > 1 {
		t.Fatal("optional dependency fixture requires single-instance optional Redis configuration")
	}
	f.redis.Close()
	response, err := f.origins[0].Client().Get(f.origins[0].URL + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8192))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"redis":"degraded"`) {
		t.Fatalf("optional Redis changed process readiness: status=%d body=%s", response.StatusCode, body)
	}
}
