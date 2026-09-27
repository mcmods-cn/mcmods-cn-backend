package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type accessLogWriterStub struct {
	started    chan struct{}
	release    chan struct{}
	startOnce  sync.Once
	executions atomic.Int64
	mu         sync.Mutex
	payloads   []string
}

func (stub *accessLogWriterStub) Exec(ctx context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	stub.executions.Add(1)
	stub.startOnce.Do(func() {
		if stub.started != nil {
			close(stub.started)
		}
	})
	if stub.release != nil {
		select {
		case <-stub.release:
		case <-ctx.Done():
			return pgconn.CommandTag{}, ctx.Err()
		}
	}
	stub.mu.Lock()
	if len(args) > 0 {
		stub.payloads = append(stub.payloads, args[0].(string))
	}
	stub.mu.Unlock()
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func testAccessLogRecord(path string) accessLogRecord {
	return accessLogRecord{
		Category: "api_access", Level: "info", Action: http.MethodGet, Target: path,
		Method: http.MethodGet, Path: path, Status: http.StatusOK, Payload: json.RawMessage(`{"bytes":2}`),
	}
}

func TestAccessLogPolicySamplesReadsButKeepsErrorsAndMutations(t *testing.T) {
	accepted := 0
	for sequence := uint64(1); sequence <= accessLogReadSampleRate*2; sequence++ {
		if decideAccessLog(http.MethodGet, "/api/v1/mods", http.StatusOK, sequence) == accessLogEnqueue {
			accepted++
		}
	}
	if accepted != 2 {
		t.Fatalf("sampled GET accepted=%d want 2", accepted)
	}
	for _, test := range []struct {
		method string
		path   string
		status int
		want   accessLogDecision
	}{
		{http.MethodPost, "/api/v1/mods", http.StatusCreated, accessLogEnqueue},
		{http.MethodGet, "/api/v1/mods", http.StatusInternalServerError, accessLogEnqueue},
		{http.MethodPost, "/api/v1/site/presence", http.StatusOK, accessLogPolicyDrop},
		{http.MethodPut, "/api/v1/messages/conversations/c1/presence", http.StatusOK, accessLogPolicyDrop},
		{http.MethodGet, "/api/v1/realtime/events", http.StatusOK, accessLogPolicyDrop},
		{http.MethodGet, "/api/v1/me/unread-summary", http.StatusOK, accessLogPolicyDrop},
		{http.MethodGet, "/api/v1/notifications/translations/t1", http.StatusOK, accessLogPolicyDrop},
		{http.MethodPost, "/api/v1/content-metrics/p1/view", http.StatusOK, accessLogPolicyDrop},
		{http.MethodPost, "/api/v1/review-locks/mod/p1/subscribe", http.StatusOK, accessLogPolicyDrop},
	} {
		if got := decideAccessLog(test.method, test.path, test.status, accessLogReadSampleRate); got != test.want {
			t.Errorf("decision for %s %s status %d = %v, want %v", test.method, test.path, test.status, got, test.want)
		}
	}
}

func TestAccessLogMiddlewareNeverWaitsForPostgres(t *testing.T) {
	writer := &accessLogWriterStub{started: make(chan struct{}), release: make(chan struct{})}
	ingestor := newAccessLogIngestor(writer, accessLogIngestorOptions{
		Capacity: 4, BatchSize: 1, FlushInterval: time.Hour, WriteTimeout: time.Second,
	})
	server := &Server{accessLogs: ingestor}
	handler := server.logAccess(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("failed"))
	}))

	started := time.Now()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/test", nil))
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("handler waited %s for the access-log writer", elapsed)
	}
	select {
	case <-writer.started:
	case <-time.After(time.Second):
		t.Fatal("access-log worker did not start its write")
	}
	close(writer.release)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ingestor.Close(shutdownCtx); err != nil {
		t.Fatal(err)
	}
}

func TestAccessLogMiddlewareAppliesReadSamplingBeforeTheQueue(t *testing.T) {
	writer := &accessLogWriterStub{}
	ingestor := newAccessLogIngestor(writer, accessLogIngestorOptions{
		Capacity: 8, BatchSize: 2, FlushInterval: time.Hour, WriteTimeout: time.Second,
	})
	server := &Server{accessLogs: ingestor}
	handler := server.logAccess(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	for index := 0; index < int(accessLogReadSampleRate*2); index++ {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/mods", nil))
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ingestor.Close(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	metrics := ingestor.Metrics()
	if metrics.SampledOut != 30 || metrics.Enqueued != 2 || metrics.Flushed != 2 || metrics.Batches != 1 {
		t.Fatalf("unexpected sampled middleware metrics: %+v", metrics)
	}
}

func TestAccessLogQueueIsBoundedAndReportsOverflow(t *testing.T) {
	writer := &accessLogWriterStub{started: make(chan struct{}), release: make(chan struct{})}
	ingestor := newAccessLogIngestor(writer, accessLogIngestorOptions{
		Capacity: 2, BatchSize: 1, FlushInterval: time.Hour, WriteTimeout: time.Second,
	})
	if !ingestor.Enqueue(testAccessLogRecord("/first")) {
		t.Fatal("first record was not enqueued")
	}
	select {
	case <-writer.started:
	case <-time.After(time.Second):
		t.Fatal("first write did not start")
	}
	if !ingestor.Enqueue(testAccessLogRecord("/second")) || !ingestor.Enqueue(testAccessLogRecord("/third")) {
		t.Fatal("records did not fill the bounded queue")
	}
	if ingestor.Enqueue(testAccessLogRecord("/overflow")) {
		t.Fatal("overflow record was accepted")
	}
	metrics := ingestor.Metrics()
	if metrics.QueueDepth != 2 || metrics.Capacity != 2 || metrics.OverflowDropped != 1 {
		t.Fatalf("unexpected overflow metrics: %+v", metrics)
	}
	close(writer.release)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ingestor.Close(shutdownCtx); err != nil {
		t.Fatal(err)
	}
}

func TestAccessLogWorkerBatchesRecordsIntoOneStatement(t *testing.T) {
	writer := &accessLogWriterStub{}
	ingestor := newAccessLogIngestor(writer, accessLogIngestorOptions{
		Capacity: 8, BatchSize: 4, FlushInterval: time.Hour, WriteTimeout: time.Second,
	})
	for index := 0; index < 4; index++ {
		if !ingestor.Enqueue(testAccessLogRecord("/batch")) {
			t.Fatal("batch record was not enqueued")
		}
	}
	deadline := time.Now().Add(time.Second)
	for ingestor.Metrics().Flushed < 4 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	metrics := ingestor.Metrics()
	if metrics.Flushed != 4 || metrics.Batches != 1 || writer.executions.Load() != 1 {
		t.Fatalf("unexpected batch result metrics=%+v executions=%d", metrics, writer.executions.Load())
	}
	writer.mu.Lock()
	var records []accessLogRecord
	err := json.Unmarshal([]byte(writer.payloads[0]), &records)
	writer.mu.Unlock()
	if err != nil || len(records) != 4 {
		t.Fatalf("batch payload records=%d err=%v", len(records), err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = ingestor.Close(shutdownCtx); err != nil {
		t.Fatal(err)
	}
}

func TestAccessLogMiddlewareHasNoSynchronousDatabaseFallback(t *testing.T) {
	logHandler, err := os.ReadFile("log_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	serverSource, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	infrastructure, err := os.ReadFile("infrastructure_metrics.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logHandler), "s.accessLogs.Capture") {
		t.Fatal("API access middleware does not use the bounded ingestor")
	}
	if strings.Contains(string(logHandler), `s.writeAppLog(context.Background(), "api_access"`) {
		t.Fatal("API access middleware retains the synchronous PostgreSQL fallback")
	}
	if !strings.Contains(string(serverSource), "newDefaultAccessLogIngestor(db)") ||
		!strings.Contains(string(serverSource), "s.accessLogs.Close(ctx)") {
		t.Fatal("access-log ingestor is not attached to the complete server lifecycle")
	}
	if !strings.Contains(string(infrastructure), `"accessLogs"`) {
		t.Fatal("access-log queue metrics are missing from infrastructure observability")
	}
}
