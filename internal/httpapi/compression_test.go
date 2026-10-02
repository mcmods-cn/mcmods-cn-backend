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

func TestCompressionNegotiatesGzipQualityForCompressibleResponses(t *testing.T) {
	server := &Server{}
	handler := server.compression(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(strings.Repeat("compressible ", 32)))
	}))
	tests := []struct {
		name       string
		encoding   string
		compressed bool
	}{
		{name: "implicit quality", encoding: "br, gzip", compressed: true},
		{name: "positive quality", encoding: "br;q=1, GZip; q=0.25", compressed: true},
		{name: "wildcard", encoding: "br;q=1, *;q=0.5", compressed: true},
		{name: "zero quality", encoding: "gzip;q=0", compressed: false},
		{name: "invalid quality", encoding: "gzip;q=abc", compressed: false},
		{name: "empty quality", encoding: "gzip;q=", compressed: false},
		{name: "out of range quality", encoding: "gzip;q=1.001", compressed: false},
		{name: "zero wildcard", encoding: "br, *;q=0", compressed: false},
		{name: "invalid wildcard", encoding: "br, *;q=invalid", compressed: false},
		{name: "explicit rejection after wildcard", encoding: "*;q=1, gzip;q=0", compressed: false},
		{name: "explicit rejection before wildcard", encoding: "gzip;q=0, *;q=1", compressed: false},
		{name: "unsupported encodings", encoding: "br, deflate", compressed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/text", nil)
			request.Header.Set("Accept-Encoding", test.encoding)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			actual := recorder.Header().Get("Content-Encoding") == "gzip"
			if actual != test.compressed {
				t.Fatalf("Accept-Encoding %q compressed=%t, want %t", test.encoding, actual, test.compressed)
			}
		})
	}
}
