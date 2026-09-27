package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSEC020AccessLogsNeverPersistQueryContent(t *testing.T) {
	writer := &accessLogWriterStub{}
	ingestor := newAccessLogIngestor(writer, accessLogIngestorOptions{
		Capacity: 8, BatchSize: 1, FlushInterval: time.Hour, WriteTimeout: time.Second,
	})
	server := &Server{accessLogs: ingestor}
	handler := server.logAccess(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/oauth/github/callback?code=oauth-code-secret&state=csrf-state-secret&access_token=bearer-secret&sk-proj-secret-name=x&safe=ordinary-value", nil)
	handler.ServeHTTP(httptest.NewRecorder(), request)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ingestor.Close(ctx); err != nil {
		t.Fatal(err)
	}
	writer.mu.Lock()
	batch := strings.Join(writer.payloads, "")
	writer.mu.Unlock()
	for _, forbidden := range []string{
		"oauth-code-secret", "csrf-state-secret", "bearer-secret", "sk-proj-secret-name", "ordinary-value",
	} {
		if strings.Contains(batch, forbidden) {
			t.Fatalf("access-log batch persisted query content %q: %s", forbidden, batch)
		}
	}

	var records []accessLogRecord
	if err := json.Unmarshal([]byte(batch), &records); err != nil || len(records) != 1 {
		t.Fatalf("decode access-log batch: records=%d err=%v", len(records), err)
	}
	var payload map[string]any
	if err := json.Unmarshal(records[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["queryPresent"] != true {
		t.Fatalf("query presence metadata missing: %#v", payload)
	}
	if _, exists := payload["query"]; exists {
		t.Fatalf("access-log payload retained query content field: %#v", payload)
	}
}
