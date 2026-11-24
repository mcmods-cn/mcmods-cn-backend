package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRealtimeSSEHasLayeredBudgetsAndFiniteLifetime(t *testing.T) {
	if maxRealtimeConnections > 4096 || maxRealtimeConnectionsPerUser > 4 || maxRealtimeConnectionsPerSession > 2 {
		t.Fatalf("unsafe realtime limits: total=%d user=%d session=%d", maxRealtimeConnections, maxRealtimeConnectionsPerUser, maxRealtimeConnectionsPerSession)
	}
	if maxRealtimeConnectionLifetime <= 0 || maxRealtimeConnectionLifetime > time.Hour {
		t.Fatalf("unsafe realtime lifetime: %s", maxRealtimeConnectionLifetime)
	}
	source, err := os.ReadFile("realtime_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{"AcquireRealtimeLease", "lease.Refresh", "maxRealtimeConnectionLifetime", "REALTIME_CONNECTION_LIMIT"} {
		if !strings.Contains(text, required) {
			t.Fatalf("SSE handler is missing %q", required)
		}
	}
}

type failingStreamWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (w *failingStreamWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}

func (*failingStreamWriter) FlushError() error { return errors.New("fixture stream closed") }

func TestRealtimeFailedFlushReleasesAdmission(t *testing.T) {
	for _, encoding := range []string{"identity", "gzip"} {
		t.Run(encoding, func(t *testing.T) {
			admission := &bug051MiddlewareLeaseCoordinator{}
			api := &Server{cache: querycache.New(config.RedisConfig{}), realtime: newRealtimeHub(), realtimeAdmission: admission}
			defer api.cache.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			claims := security.Claims{Subject: 51, SessionID: "fixture-stream", ExpiresAt: time.Now().Add(time.Minute).Unix()}
			r := httptest.NewRequest(http.MethodGet, "/api/v1/realtime/events", nil).WithContext(context.WithValue(ctx, claimsContextKey, claims))
			r.Header.Set("Accept-Encoding", encoding)
			writer := &failingStreamWriter{ResponseRecorder: httptest.NewRecorder()}
			done := make(chan struct{})
			go func() {
				defer close(done)
				api.compression(api.logAccess(http.HandlerFunc(api.realtimeEvents))).ServeHTTP(writer, r)
			}()
			select {
			case <-done:
			case <-time.After(time.Second):
				cancel()
				<-done
				t.Fatal("handler retained the admission after the transport flush failed")
			}
			if writer.deadline.IsZero() || writer.deadline.After(time.Now().Add(time.Minute)) {
				t.Fatalf("failed frame has no bounded write deadline: %v", writer.deadline)
			}
			if admission.acquiredID == "" || admission.releasedID != admission.acquiredID {
				t.Fatalf("admission not released after flush error: %#v", admission)
			}
			if connections, users := api.realtime.connectionCount(); connections != 0 || users != 0 {
				t.Fatalf("subscriptions after flush error: %d/%d", connections, users)
			}
		})
	}
}
