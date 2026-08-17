package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiveProbeDoesNotRequireDependencies(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodGet, "/live", nil)
	response := httptest.NewRecorder()
	server.live(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("live status = %d", response.Code)
	}
}

func TestReadyFailsClosedWithoutPostgreSQL(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	response := httptest.NewRecorder()
	server.ready(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready status = %d", response.Code)
	}
}

func TestLegacyHealthRouteIsRemoved(t *testing.T) {
	server := &Server{mux: http.NewServeMux()}
	server.routes()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	server.mux.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("legacy health status = %d, want %d", response.Code, http.StatusNotFound)
	}
}
