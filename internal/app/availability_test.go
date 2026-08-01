package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestAvailabilityHandlerServesHealthDuringStartup(t *testing.T) {
	handler := newAvailabilityHandler(config.Config{Env: "test", FrontendOrigin: "https://frontend.example"})
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set("Origin", "https://frontend.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("startup health status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://frontend.example" {
		t.Fatalf("health CORS origin = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("health cache policy = %q", got)
	}
	var envelope struct {
		Data  runtimeHealthSnapshot `json:"data"`
		Error string                `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Ready || envelope.Data.Status != "error" || len(envelope.Data.Issues) != 1 {
		t.Fatalf("unexpected startup health payload: %+v", envelope.Data)
	}
	if envelope.Error != availabilityErrorMessage {
		t.Fatalf("startup health error = %q", envelope.Error)
	}
}

func TestAvailabilityHandlerGatesTrafficAndRecovers(t *testing.T) {
	forwarded := 0
	handler := newAvailabilityHandler(config.Config{Env: "test"})
	handler.setHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded++
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.resolveIssue("startup")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/mods", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || forwarded != 1 {
		t.Fatalf("ready request status=%d forwarded=%d", response.Code, forwarded)
	}

	handler.reportIssue("database", "database_unavailable", "database is unavailable")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || forwarded != 1 {
		t.Fatalf("degraded request status=%d forwarded=%d", response.Code, forwarded)
	}
	if got := response.Header().Get("Retry-After"); got != "5" {
		t.Fatalf("Retry-After = %q", got)
	}

	handler.resolveIssue("database")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || forwarded != 2 {
		t.Fatalf("recovered request status=%d forwarded=%d", response.Code, forwarded)
	}
}

func TestRuntimeRetryDelayIsBounded(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{{1, 2 * time.Second}, {2, 4 * time.Second}, {3, 8 * time.Second}, {5, 30 * time.Second}, {20, 30 * time.Second}}
	for _, test := range tests {
		if got := runtimeRetryDelay(test.attempt); got != test.want {
			t.Errorf("runtimeRetryDelay(%d) = %s, want %s", test.attempt, got, test.want)
		}
	}
}
