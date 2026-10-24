package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

func TestAuditMSG001MissingDatabaseReturns503WithoutLeakingAdmission(t *testing.T) {
	server := &Server{cache: querycache.New(config.RedisConfig{}), realtime: newRealtimeHub()}
	t.Cleanup(func() { server.cache.Close() })
	ctx := context.WithValue(context.Background(), claimsContextKey, security.Claims{Subject: 51, SessionID: "nil-database-session"})
	// More requests than the per-session budget prove rejected admissions release
	// their cache leases, rather than turning the third request into a false429.
	for attempt := 0; attempt <= maxRealtimeConnectionsPerSession; attempt++ {
		response := httptest.NewRecorder()
		server.realtimeEvents(response, httptest.NewRequest(http.MethodGet, "/api/v1/realtime/events", nil).WithContext(ctx))
		var body struct{ Code, Error string }
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusServiceUnavailable || body.Code != "HTTP_503" || body.Error != "realtime admission unavailable" {
			t.Fatalf("attempt%d: unavailable database status=%d body=%s", attempt, response.Code, response.Body.String())
		}
		if response.Flushed || strings.Contains(response.Body.String(), ": connected") {
			t.Fatalf("database refusal opened a stream: %s", response.Body.String())
		}
	}
	if users, connections := server.realtime.connectionCount(); users != 0 || connections != 0 {
		t.Fatalf("database refusal left subscribers: users=%d connections=%d", users, connections)
	}
}

func TestAuditMSG003OptionalRealRedisKeepsReadinessAndDurableAdmission(t *testing.T) {
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
	cfg.Redis.Enabled, cfg.Redis.Required, cfg.Redis.RateLimitFailClosed = true, false, false
	cfg.ReplicaCount = 1
	cfg.Redis.Addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	cfg.Redis.Namespace = "audit-optional-real-" + randomCatalogPublicID()
	cfg.Redis.AuthSessionCacheEnabled, cfg.Redis.RBACCacheEnabled = false, false
	cfg.Redis.DialTimeout, cfg.Redis.ReadTimeout, cfg.Redis.WriteTimeout = 50*time.Millisecond, 50*time.Millisecond, 50*time.Millisecond
	q := queue.New(f.ctx, cfg.NATS)
	t.Cleanup(q.Close)
	server := NewServer(f.ctx, cfg, f.db, q, nil, nil, nil)
	t.Cleanup(func() { server.Shutdown(context.Background()); server.cache.Close() })
	origin := httptest.NewServer(server)
	t.Cleanup(origin.Close)
	request := func(path string, want int) *http.Response {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, origin.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+f.editor)
		response, err := origin.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			response.Body.Close()
			t.Fatalf("%s=%d want%d", path, response.StatusCode, want)
		}
		return response
	}
	request("/ready", http.StatusOK).Body.Close()
	stopRedis()
	response := request("/ready", http.StatusOK)
	body, err := io.ReadAll(io.LimitReader(response.Body, 8192))
	response.Body.Close()
	if err != nil || !strings.Contains(string(body), `"redis":"degraded"`) {
		t.Fatalf("optional real Redis readiness body=%s err=%v", body, err)
	}
	request("/live", http.StatusOK).Body.Close()
	stream := request("/api/v1/realtime/events", http.StatusOK)
	defer stream.Body.Close()
	var leases int
	if err = f.db.QueryRow(f.ctx, `select count(*) from realtime_connection_leases`).Scan(&leases); err != nil || leases != 1 {
		t.Fatalf("optional Redis stream did not retain PostgreSQL admission: leases=%d err=%v", leases, err)
	}
	stream.Body.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err = f.db.QueryRow(f.ctx, `select count(*) from realtime_connection_leases`).Scan(&leases); err != nil {
			t.Fatal(err)
		}
		if leases == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("optional Redis stream release leaked%d PostgreSQL leases", leases)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
