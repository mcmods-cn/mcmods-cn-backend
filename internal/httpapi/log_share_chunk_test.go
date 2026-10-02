package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLogShareChunkRouteAndConcurrencyBudget(t *testing.T) {
	server := &Server{mux: http.NewServeMux(), logShareBodyReads: make(chan struct{}, 1)}
	server.routes()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/log-shares/s/public-code/entries/7/content", nil)
	_, pattern := server.mux.Handler(request)
	if pattern != "GET /api/v1/log-shares/s/{code}/entries/{entryIndex}/content" {
		t.Fatalf("log share chunk route is not registered: %q", pattern)
	}
	firstResponse := httptest.NewRecorder()
	release, ok := server.acquireLogShareBodyRead(firstResponse, request, "test", 1)
	if !ok {
		t.Fatalf("first body slot rejected: %d", firstResponse.Code)
	}
	secondResponse := httptest.NewRecorder()
	if _, secondOK := server.acquireLogShareBodyRead(secondResponse, request, "test", 1); secondOK || secondResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("concurrency budget was not enforced: ok=%v status=%d", secondOK, secondResponse.Code)
	}
	release()
}

func TestLogShareChunkCursorIsStrictAndEntryScoped(t *testing.T) {
	scope := logShareChunkCursorScope("public-code", 7)
	cursor := encodeLogShareChunkCursor(scope, maxLogShareChunkRunes)
	offset, err := decodeLogShareChunkCursor(cursor, scope)
	if err != nil || offset != maxLogShareChunkRunes {
		t.Fatalf("decode chunk cursor: offset=%d err=%v", offset, err)
	}
	if _, err = decodeLogShareChunkCursor(cursor, logShareChunkCursorScope("public-code", 8)); err == nil {
		t.Fatal("chunk cursor crossed an entry boundary")
	}
	if _, err = decodeLogShareChunkCursor(cursor+"garbage", scope); err == nil {
		t.Fatal("chunk cursor accepted trailing data")
	}
}

func TestLogShareChunkPageHasHardJSONBudget(t *testing.T) {
	scope := logShareChunkCursorScope("public-code", 0)
	page := makeLogShareChunkPage(strings.Repeat("<", maxLogShareChunkRunes+17), 0, scope)
	if utf8.RuneCountInString(page.Text) != maxLogShareChunkRunes || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("unexpected first chunk: runes=%d hasMore=%v next=%q", utf8.RuneCountInString(page.Text), page.HasMore, page.NextCursor)
	}
	offset, err := decodeLogShareChunkCursor(page.NextCursor, scope)
	if err != nil || offset != maxLogShareChunkRunes {
		t.Fatalf("next cursor: offset=%d err=%v", offset, err)
	}
	payload, err := json.Marshal(apiResponse{Data: page})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > maxLogShareChunkResponseBytes {
		t.Fatalf("escaped chunk response exceeded budget: %d > %d", len(payload), maxLogShareChunkResponseBytes)
	}
}

func TestLogShareEntryMetadataNeverCarriesText(t *testing.T) {
	payload, err := json.Marshal(logShareEntryMetadata{Index: 2, Name: "latest.log", ByteSize: 20 << 20, LineCount: 50_000})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "text") || len(payload) > 1024 {
		t.Fatalf("entry metadata leaked body or exceeded its row budget: %s", payload)
	}
}
