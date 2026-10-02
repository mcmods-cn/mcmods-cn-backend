package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

// Network requests pass through NewServer, real PostgreSQL sessions and every
// HTTP wrapper. NATS is the official server. Only Redis is an explicit Lua-
// capable test double; this does not claim production Redis/load validation.
func TestTEST023RealtimeHTTPAuthenticationBudgetsIsolationAndReleaseIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := newTEST018IsolatedDatabase(t, ctx)
	broker := newTEST027Broker(t, false, false)
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared_leases_%t", shared), func(t *testing.T) {
			cfg := config.Load()
			cfg.JWTSecret = "test023-only-jwt-signing-secret"
			cfg.SettingsEncryptionKey = "test023-only-encryption-key-32-bytes"
			cfg.AntiAbuse.Enabled = false
			cfg.Redis.Enabled = shared
			if shared {
				redisServer := miniredis.RunT(t)
				cfg.Redis.Addr, cfg.Redis.Password = redisServer.Addr(), ""
				cfg.Redis.Prefix, cfg.Redis.Namespace = "test023", "shared-http"
				cfg.Redis.DialTimeout = 100 * time.Millisecond
				cfg.Redis.ReadTimeout, cfg.Redis.WriteTimeout = 100*time.Millisecond, 100*time.Millisecond
			}
			cfg.NATS = config.NATSConfig{Enabled: true, URL: broker.ClientURL(), Username: "test027", Password: "test027-password", SubjectPrefix: fmt.Sprintf("test023_%t", shared), Realtime: true}
			servers := make([]*Server, 2)
			origins := make([]*httptest.Server, 2)
			activeRequests := make([]atomic.Int64, 2)
			for index := range servers {
				queueClient := queue.New(ctx, cfg.NATS)
				t.Cleanup(queueClient.Close)
				servers[index] = NewServer(ctx, cfg, pool, queueClient, nil, nil, nil)
				server := servers[index]
				t.Cleanup(func() {
					shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer shutdownCancel()
					if err := server.Shutdown(shutdownCtx); err != nil {
						t.Errorf("shutdown realtime server: %v", err)
					}
					if err := server.cache.Close(); err != nil {
						t.Errorf("close realtime cache: %v", err)
					}
				})
				active := &activeRequests[index]
				origins[index] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					active.Add(1)
					defer active.Add(-1)
					server.ServeHTTP(w, r)
				}))
				t.Cleanup(origins[index].Close)
				if !queueClient.Status().RealtimeReady {
					t.Fatalf("official broker subscription is not ready: %#v", queueClient.Status())
				}
			}
			userID, _, token := createTEST044User(t, ctx, pool, cfg, fmt.Sprintf("023a-%t", shared), "UTC")
			otherID, _, otherToken := createTEST044User(t, ctx, pool, cfg, fmt.Sprintf("023b-%t", shared), "UTC")
			secondToken := mintTEST048Token(t, ctx, pool, cfg, userID)
			thirdToken := mintTEST048Token(t, ctx, pool, cfg, userID)
			peer := 0
			if shared {
				peer = 1
			}
			requestStatus := func(token string, want int) {
				t.Helper()
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, origins[peer].URL+"/api/v1/realtime/events", nil)
				if err != nil {
					t.Fatal(err)
				}
				if token != "" {
					request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: token})
				}
				response, err := origins[peer].Client().Do(request)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				// Do not hang reading a mistakenly admitted SSE request.
				if response.StatusCode != want {
					t.Fatalf("SSE status=%d want=%d", response.StatusCode, want)
				}
				raw, err := io.ReadAll(io.LimitReader(response.Body, 8192))
				if err != nil {
					t.Fatal(err)
				}
				if want == http.StatusTooManyRequests && (!strings.Contains(string(raw), "REALTIME_CONNECTION_LIMIT") || response.Header.Get("Retry-After") != "30") {
					t.Fatalf("missing limit code/retry contract: headers=%v body=%s", response.Header, raw)
				}
			}
			requestStatus("", http.StatusUnauthorized)
			requestStatus("not-a-valid-jwt", http.StatusUnauthorized)
			streams := []*test023SSEStream{
				openTEST023Stream(t, ctx, origins[0], token),
				openTEST023Stream(t, ctx, origins[peer], token),
			}
			requestStatus(token, http.StatusTooManyRequests)
			streams = append(streams, openTEST023Stream(t, ctx, origins[0], secondToken), openTEST023Stream(t, ctx, origins[peer], secondToken))
			requestStatus(thirdToken, http.StatusTooManyRequests)
			other := openTEST023Stream(t, ctx, origins[peer], otherToken)
			if rejected, _ := servers[peer].realtime.deliveryStats(); rejected != 2 {
				t.Fatalf("rejected connections=%d want=2", rejected)
			}
			assertDelivery := func(publisher *Server, eventType string) {
				t.Helper()
				publisher.publishRealtimeUser(userID, eventType, map[string]string{"text": "private-to-A"})
				identity := ""
				for _, stream := range streams {
					frame := stream.receive(t)
					var body map[string]string
					if err := json.Unmarshal([]byte(frame.data), &body); err != nil || body["text"] != "private-to-A" || frame.kind != eventType || frame.id == "" {
						t.Fatalf("unexpected private SSE frame=%#v err=%v", frame, err)
					}
					if identity != "" && frame.id != identity {
						t.Fatalf("cross-instance event identity diverged: %q / %q", identity, frame.id)
					}
					identity = frame.id
				}
			}
			assertDelivery(servers[0], "message.created")
			assertDelivery(servers[1], "notification.created")
			other.assertEmpty(t)
			servers[1].publishRealtimeUser(otherID, "message.other", map[string]string{"text": "private-to-B"})
			if frame := other.receive(t); frame.kind != "message.other" || !strings.Contains(frame.data, "private-to-B") {
				t.Fatalf("wrong other-user SSE frame: %#v", frame)
			}
			for _, stream := range streams {
				stream.assertEmpty(t) // no own NATS echo, peer duplicate or user-B body
			}
			servers[0].publishRealtimeUser(userID, "cannot.encode", func() {})
			assertDelivery(servers[0], "message.after-malformed")
			_, dropped := servers[0].realtime.deliveryStats()
			wantDropped := uint64(4)
			if shared {
				wantDropped = 2
			}
			if dropped != wantDropped {
				t.Fatalf("malformed-event drops=%d want=%d", dropped, wantDropped)
			}
			for _, stream := range streams {
				stream.close(t)
			}
			other.close(t)
			waitTEST023Connections(t, servers, activeRequests, 0)
			// Same session, including another replica, can immediately reuse the
			// capacity after request cancellation (not merely after the 90s TTL).
			replacement := openTEST023Stream(t, ctx, origins[peer], token)
			replacement.close(t)
			waitTEST023Connections(t, servers, activeRequests, 0)
		})
	}
}

func TestTEST023RealtimeHubBoundsSlowConsumersAndUnsubscribe(t *testing.T) {
	hub := newRealtimeHub()
	events, unsubscribe := hub.subscribe(42)
	for index := 0; index < 34; index++ {
		hub.publish(42, realtimeEvent{ID: fmt.Sprint(index)})
	}
	if rejected, dropped := hub.deliveryStats(); rejected != 0 || dropped != 2 {
		t.Fatalf("bounded slow-consumer stats=%d/%d want=0/2", rejected, dropped)
	}
	for index := 0; index < 32; index++ {
		select {
		case event := <-events:
			if event.ID != fmt.Sprint(index) {
				t.Fatalf("buffer order=%s want=%d", event.ID, index)
			}
		default:
			t.Fatalf("buffer lost event %d", index)
		}
	}
	unsubscribe()
	unsubscribe()
	if connections, users := hub.connectionCount(); connections != 0 || users != 0 {
		t.Fatalf("unsubscribed connection remains: %d/%d", connections, users)
	}
	hub.publish(42, realtimeEvent{ID: "after-close"})
	select {
	case event := <-events:
		t.Fatalf("closed subscription still receives: %#v", event)
	default:
	}
}

type test023SSEFrame struct{ id, kind, data string }
type test023SSEStream struct {
	body   io.ReadCloser
	cancel context.CancelFunc
	frames chan test023SSEFrame
	done   chan struct{}
	once   sync.Once
}

func openTEST023Stream(t *testing.T, ctx context.Context, origin *httptest.Server, token string) *test023SSEStream {
	t.Helper()
	streamCtx, cancel := context.WithCancel(ctx)
	request, err := http.NewRequestWithContext(streamCtx, http.MethodGet, origin.URL+"/api/v1/realtime/events", nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: token})
	response, err := origin.Client().Do(request)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" || response.Header.Get("X-Accel-Buffering") != "no" || !strings.Contains(response.Header.Get("Cache-Control"), "no-transform") || response.Header.Get("Content-Encoding") != "" {
		cancel()
		_ = response.Body.Close()
		t.Fatalf("complete middleware did not admit unbuffered SSE: status=%d headers=%v", response.StatusCode, response.Header)
	}
	stream := &test023SSEStream{body: response.Body, cancel: cancel, frames: make(chan test023SSEFrame, 8), done: make(chan struct{})}
	t.Cleanup(func() { stream.close(t) })
	ready := make(chan struct{})
	go func() {
		defer close(stream.done)
		scanner := bufio.NewScanner(response.Body)
		frame := test023SSEFrame{}
		connected := false
		for scanner.Scan() {
			line := scanner.Text()
			if line == ": connected" && !connected {
				connected = true
				close(ready)
			}
			switch {
			case line == "" && frame.id != "":
				select {
				case stream.frames <- frame:
				case <-streamCtx.Done():
					return
				}
				frame = test023SSEFrame{}
			case strings.HasPrefix(line, "id: "):
				frame.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				frame.kind = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				frame.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	select {
	case <-ready:
	case <-stream.done:
		t.Fatal("SSE closed before the initial comment was flushed")
	case <-time.After(2 * time.Second):
		t.Fatal("initial SSE comment did not reach the network")
	}
	return stream
}

func (stream *test023SSEStream) receive(t *testing.T) test023SSEFrame {
	t.Helper()
	select {
	case frame := <-stream.frames:
		return frame
	case <-stream.done:
		t.Fatal("SSE stream closed before the expected event")
	case <-time.After(2 * time.Second):
		t.Fatal("expected SSE event did not reach the network")
	}
	return test023SSEFrame{}
}

func (stream *test023SSEStream) assertEmpty(t *testing.T) {
	t.Helper()
	select {
	case frame := <-stream.frames:
		t.Fatalf("duplicate or wrong-user SSE event: %#v", frame)
	case <-stream.done:
		t.Fatal("SSE unexpectedly closed")
	case <-time.After(150 * time.Millisecond):
	}
}

func (stream *test023SSEStream) close(t *testing.T) {
	t.Helper()
	stream.once.Do(func() { stream.cancel(); _ = stream.body.Close() })
	select {
	case <-stream.done:
	case <-time.After(time.Second):
		t.Error("cancelled SSE reader did not exit")
	}
}

func waitTEST023Connections(t *testing.T, servers []*Server, activeRequests []atomic.Int64, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		count := 0
		inflight := int64(0)
		for index, server := range servers {
			connections, _ := server.realtime.connectionCount()
			count += connections
			inflight += activeRequests[index].Load()
		}
		// Observe return from the real handler, not only Hub unsubscribe: its
		// deferred shared lease release must also have completed.
		if count == want && inflight == int64(want) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("SSE subscriptions did not settle to %d", want)
}
