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
