package httpapi

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompressionGzipsJSONResponses(t *testing.T) {
	server := &Server{}
	handler := server.compression(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"message": strings.Repeat("normalized ", 32)})
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	request.Header.Set("Accept-Encoding", "br, gzip")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q", recorder.Header().Get("Content-Encoding"))
	}
	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "normalized") {
		t.Fatalf("unexpected payload %q", payload)
	}
}

func TestCompressionSkipsBinaryResponsesAndRejectedGzip(t *testing.T) {
	server := &Server{}
	handler := server.compression(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	for _, encoding := range []string{"gzip", "gzip;q=0"} {
		request := httptest.NewRequest(http.MethodGet, "/image", nil)
		request.Header.Set("Accept-Encoding", encoding)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Header().Get("Content-Encoding") != "" {
			t.Fatalf("encoding %q unexpectedly compressed binary response", encoding)
		}
	}
}
