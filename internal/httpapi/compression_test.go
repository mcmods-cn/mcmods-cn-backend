package httpapi

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type compressionFlushErrorWriter struct {
	*httptest.ResponseRecorder
	writeError error
	flushError error
	flushCalls int
}

func (w *compressionFlushErrorWriter) Write(payload []byte) (int, error) {
	if w.writeError != nil {
		return 0, w.writeError
	}
	return w.ResponseRecorder.Write(payload)
}

func (w *compressionFlushErrorWriter) FlushError() error {
	w.flushCalls++
	return w.flushError
}

func TestCompressionFlushPropagatesTransportErrors(t *testing.T) {
	for _, compress := range []bool{false, true} {
		t.Run(map[bool]string{false: "uncompressed", true: "gzip"}[compress], func(t *testing.T) {
			failure := errors.New("controlled transport flush failure")
			underlying := &compressionFlushErrorWriter{ResponseRecorder: httptest.NewRecorder(), flushError: failure}
			server := &Server{}
			handler := server.compression(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				contentType := "application/octet-stream"
				if compress {
					contentType = "text/event-stream"
				}
				w.Header().Set("Content-Type", contentType)
				if _, err := w.Write([]byte("data: event\n\n")); err != nil {
					t.Fatal(err)
				}
				if err := http.NewResponseController(w).Flush(); !errors.Is(err, failure) {
					t.Fatalf("Flush error=%v, want controlled transport error", err)
				}
			}))
			request := httptest.NewRequest(http.MethodGet, "/events", nil)
			request.Header.Set("Accept-Encoding", "gzip")
			handler.ServeHTTP(underlying, request)
			if underlying.flushCalls != 1 {
				t.Fatalf("underlying FlushError calls=%d, want 1", underlying.flushCalls)
			}
		})
	}
}

func TestCompressionFlushPropagatesGzipWriteErrors(t *testing.T) {
	failure := errors.New("controlled compressed write failure")
	underlying := &compressionFlushErrorWriter{ResponseRecorder: httptest.NewRecorder(), writeError: failure}
	writer := &compressedResponseWriter{ResponseWriter: underlying, statusCode: http.StatusOK}
	writer.Header().Set("Content-Type", "text/event-stream")
	if err := http.NewResponseController(writer).Flush(); !errors.Is(err, failure) {
		t.Fatalf("Flush error=%v, want controlled gzip write error", err)
	}
	if underlying.flushCalls != 0 {
		t.Fatalf("gzip failure still called underlying FlushError %d times", underlying.flushCalls)
	}
}

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

func TestCompressionHonorsExplicitGzipRejection(t *testing.T) {
	server := &Server{}
	handler := server.compression(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"message": "readable without gzip"})
	}))
	for _, encoding := range []string{"gzip;q=0, *;q=1", "*;q=1, gzip;q=0", "gzip;q=invalid", "gzip;q=2"} {
		t.Run(encoding, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
			request.Header.Set("Accept-Encoding", encoding)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Header().Get("Content-Encoding") != "" || !strings.Contains(response.Body.String(), "readable without gzip") {
				t.Fatalf("rejected encoding %q produced encoding=%q body=%q", encoding, response.Header().Get("Content-Encoding"), response.Body.String())
			}
		})
	}
}
