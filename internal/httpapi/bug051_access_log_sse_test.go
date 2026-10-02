package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

type bug051CancelOnFlushWriter struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (writer *bug051CancelOnFlushWriter) Flush() {
	writer.ResponseRecorder.Flush()
	writer.cancel()
}

func TestBUG051RealtimeSSESurvivesTheCompleteMiddlewareChain(t *testing.T) {
	server := &Server{
		cache:    querycache.New(config.RedisConfig{}),
		realtime: newRealtimeHub(),
	}
	handler := server.securityHeaders(server.cors(server.cookieRequestOrigin(server.yggdrasilALI(
		server.compression(server.logAccess(server.botTraffic(http.HandlerFunc(server.realtimeEvents)))),
	))))

	requestContext := context.WithValue(context.Background(), claimsContextKey, security.Claims{
		Subject:   51,
		SessionID: "bug051-session",
	})
	requestContext, cancel := context.WithCancel(requestContext)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/realtime/events", nil).WithContext(requestContext)
	response := &bug051CancelOnFlushWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("complete middleware chain returned %d instead of opening SSE: %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("unexpected SSE content type %q", contentType)
	}
	if !response.Flushed || !strings.Contains(response.Body.String(), ": connected\n\n") {
		t.Fatalf("initial SSE frame was not flushed: flushed=%v body=%q", response.Flushed, response.Body.String())
	}
}

var (
	errBUG051Hijacked = errors.New("bug051 hijack sentinel")
	errBUG051Pushed   = errors.New("bug051 push sentinel")
)

type bug051OptionalWriter struct {
	header   http.Header
	body     bytes.Buffer
	status   int
	flushed  bool
	hijacked bool
	pushed   bool
	readFrom bool
}

func (writer *bug051OptionalWriter) Header() http.Header {
	return writer.header
}

func (writer *bug051OptionalWriter) WriteHeader(status int) {
	writer.status = status
}

func (writer *bug051OptionalWriter) Write(payload []byte) (int, error) {
	return writer.body.Write(payload)
}

func (writer *bug051OptionalWriter) Flush() {
	writer.flushed = true
}

func (writer *bug051OptionalWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	writer.hijacked = true
	return nil, nil, errBUG051Hijacked
}

func (writer *bug051OptionalWriter) Push(string, *http.PushOptions) error {
	writer.pushed = true
	return errBUG051Pushed
}

func (writer *bug051OptionalWriter) ReadFrom(source io.Reader) (int64, error) {
	writer.readFrom = true
	return io.Copy(&writer.body, source)
}

func TestBUG051AccessLogRecorderPreservesOptionalWriterSemantics(t *testing.T) {
	underlying := &bug051OptionalWriter{header: make(http.Header)}
	recorder := &responseRecorder{ResponseWriter: underlying}
	wrapped := http.ResponseWriter(recorder)

	flusher, flushOK := wrapped.(http.Flusher)
	if !flushOK {
		t.Error("access-log recorder dropped http.Flusher")
	} else {
		flusher.Flush()
	}
	hijacker, hijackOK := wrapped.(http.Hijacker)
	if !hijackOK {
		t.Error("access-log recorder dropped http.Hijacker")
	} else if _, _, err := hijacker.Hijack(); !errors.Is(err, errBUG051Hijacked) {
		t.Errorf("hijack result was not forwarded: %v", err)
	}
	pusher, pushOK := wrapped.(http.Pusher)
	if !pushOK {
		t.Error("access-log recorder dropped http.Pusher")
	} else if err := pusher.Push("/asset", nil); !errors.Is(err, errBUG051Pushed) {
		t.Errorf("push result was not forwarded: %v", err)
	}
	readerFrom, readFromOK := wrapped.(io.ReaderFrom)
	if !readFromOK {
		t.Error("access-log recorder dropped io.ReaderFrom")
	} else if _, err := readerFrom.ReadFrom(strings.NewReader("streamed")); err != nil {
		t.Fatal(err)
	}

	if !underlying.flushed || !underlying.hijacked || !underlying.pushed || !underlying.readFrom {
		t.Errorf("optional methods did not reach the underlying writer: %#v", underlying)
	}
	if recorder.status != http.StatusOK || recorder.bytes != len("streamed") {
		t.Errorf("streamed response accounting is wrong: status=%d bytes=%d", recorder.status, recorder.bytes)
	}
}
