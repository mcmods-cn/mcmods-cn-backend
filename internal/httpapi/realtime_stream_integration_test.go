package httpapi

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestRealtimeStreamThroughCompleteMiddlewareAndRevocationIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	key := "stream_" + randomHex(8)
	var userID int64
	var publicID string
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,status)
		values($1,$2,'fixture',true,'active') returning id,public_id`, key, key+"@example.test").Scan(&userID, &publicID); err != nil {
		t.Fatal(err)
	}
	cfg.JWTSecret = randomHex(32)
	cfg.Redis.Enabled, cfg.NATS.Enabled, cfg.NATS.Realtime = false, false, false
	cache := querycache.New(cfg.Redis)
	t.Cleanup(func() { _ = cache.Close() })
	api := httptest.NewUnstartedServer(NewServer(cfg, pool, nil, cache, nil, nil))
	// Exercise the real server's ordinary WriteTimeout independently of the
	// 20-second heartbeat. A stream must refresh a bounded deadline per frame.
	api.Config.WriteTimeout = time.Second
	api.Start()
	t.Cleanup(api.Close)
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}, Timeout: 25 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	type stream struct {
		claims security.Claims
		body   io.ReadCloser
		reader *bufio.Reader
	}
	streams := make([]stream, 3)
	for index := range streams {
		claims, err := security.NewClaims(publicID, key, key+"@example.test", 1, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at)
			values($1,$2,1,$3)`, security.SessionFingerprint(claims.SessionID), userID, time.Unix(claims.ExpiresAt, 0)); err != nil {
			t.Fatal(err)
		}
		token, err := security.SignToken(cfg.JWTSecret, claims)
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, api.URL+"/api/v1/realtime/events", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Accept-Encoding", "identity")
		if index == 1 {
			request.Header.Set("Accept-Encoding", "gzip")
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = response.Body.Close() })
		if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("middleware blocked streaming: status=%d contentType=%q", response.StatusCode, response.Header.Get("Content-Type"))
		}
		var decoded io.Reader = response.Body
		if index == 1 {
			if response.Header.Get("Content-Encoding") != "gzip" {
				t.Fatal("gzip stream was not tested")
			}
			reader, err := gzip.NewReader(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reader.Close() })
			decoded = reader
		}
		reader := bufio.NewReader(decoded)
		line, err := reader.ReadString('\n')
		if err != nil || line != ": connected\n" {
			t.Fatalf("initial frame was not flushed: line=%q error=%v", line, err)
		}
		if _, err = reader.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
		streams[index] = stream{claims: claims, body: response.Body, reader: reader}
	}
	if _, err := pool.Exec(ctx, `update auth_sessions set revoked_at=now() where session_hash=$1`, security.SessionFingerprint(streams[2].claims.SessionID)); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, len(streams))
	for index, connection := range streams {
		go func() {
			line, err := connection.reader.ReadString('\n')
			if index == 2 {
				if err != io.EOF {
					results <- fmt.Errorf("revoked stream did not close: line=%q error=%v", line, err)
					return
				}
			} else if err != nil || line != ": heartbeat\n" {
				results <- fmt.Errorf("heartbeat was not flushed: line=%q error=%v", line, err)
				return
			}
			_ = connection.body.Close()
			results <- nil
		}()
	}
	for range streams {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
}

type realtimeNonStreamingWriter struct {
	header http.Header
	status int
	body   strings.Builder
}

func (w *realtimeNonStreamingWriter) Header() http.Header            { return w.header }
func (w *realtimeNonStreamingWriter) WriteHeader(status int)         { w.status = status }
func (w *realtimeNonStreamingWriter) Write(body []byte) (int, error) { return w.body.Write(body) }

func TestRealtimeStreamRejectsNonStreamingUnderlyingWriter(t *testing.T) {
	writer := &realtimeNonStreamingWriter{header: make(http.Header)}
	server := &Server{realtime: newRealtimeHub()}
	server.realtimeEvents(&responseRecorder{ResponseWriter: writer}, httptest.NewRequest(http.MethodGet, "/api/v1/realtime/events", nil))
	if writer.status != http.StatusNotImplemented {
		t.Fatalf("nonstreaming writer status=%d", writer.status)
	}
	if connections, _ := server.realtime.connectionCount(); connections != 0 {
		t.Fatal("unsupported stream left a subscription")
	}
}

func TestRealtimeStreamUnsubscribesAfterFlushFailure(t *testing.T) {
	writer := &realtimeFlushFailureWriter{ResponseRecorder: httptest.NewRecorder()}
	server := &Server{realtime: newRealtimeHub()}
	server.realtimeEvents(&responseRecorder{ResponseWriter: writer}, httptest.NewRequest(http.MethodGet, "/api/v1/realtime/events", nil).WithContext(context.Background()))
	if connections, _ := server.realtime.connectionCount(); connections != 0 {
		t.Fatal("failed flush left a subscription")
	}
}

type realtimeFlushFailureWriter struct{ *httptest.ResponseRecorder }

func (*realtimeFlushFailureWriter) FlushError() error {
	return fmt.Errorf("synthetic closed connection")
}
