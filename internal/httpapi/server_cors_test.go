package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/config"
)

func TestCORSPreflightAllowsWriteRequestHeaders(t *testing.T) {
	t.Parallel()

	server := &Server{cfg: config.Config{FrontendOrigin: "http://localhost:3000"}}
	nextCalled := false
	handler := server.cors(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))

	request := httptest.NewRequest(http.MethodOptions, "/api/v1/comment-targets/recipe_type/puzsju8dk/comments", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "authorization,content-type,idempotency-key")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, response.Code)
	}
	if nextCalled {
		t.Fatal("preflight request reached the application handler")
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("unexpected allowed origin %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("unexpected credentials setting %q", got)
	}

	allowedHeaders := make(map[string]bool)
	for _, header := range strings.Split(response.Header().Get("Access-Control-Allow-Headers"), ",") {
		allowedHeaders[strings.ToLower(strings.TrimSpace(header))] = true
	}
	for _, required := range []string{"authorization", "content-type", "idempotency-key"} {
		if !allowedHeaders[required] {
			t.Errorf("required header %q is not allowed; got %q", required, response.Header().Get("Access-Control-Allow-Headers"))
		}
	}
}

func TestCORSExposesBackendResponseMarker(t *testing.T) {
	t.Parallel()

	server := &Server{cfg: config.Config{FrontendOrigin: "http://localhost:3000"}}
	handler := server.cors(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusServiceUnavailable, "optional dependency unavailable")
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/example", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if got := response.Header().Get(backendResponseHeader); got != "1" {
		t.Fatalf("backend response marker = %q, want 1", got)
	}
	exposed := make(map[string]bool)
	for _, header := range strings.Split(response.Header().Get("Access-Control-Expose-Headers"), ",") {
		exposed[strings.ToLower(strings.TrimSpace(header))] = true
	}
	if !exposed[strings.ToLower(backendResponseHeader)] {
		t.Fatalf("backend marker is not exposed to browsers; got %q", response.Header().Get("Access-Control-Expose-Headers"))
	}
}
