package searchindex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"mcmods-cn-backend/internal/config"
)

func TestSearchRebuildImportRequestsStayWithinByteBudget(t *testing.T) {
	var mutex sync.Mutex
	requestCount := 0
	maximumRequestBytes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		lines := strings.Count(strings.TrimSpace(string(body)), "\n") + 1
		mutex.Lock()
		requestCount++
		maximumRequestBytes = max(maximumRequestBytes, len(body))
		mutex.Unlock()
		for range lines {
			_, _ = io.WriteString(w, "{\"success\":true}\n")
		}
	}))
	defer server.Close()

	client := New(config.TypesenseConfig{Enabled: true, URL: server.URL, APIKey: "test", Timeout: 5 * time.Second})
	worker := NewWorker(nil, client)
	documents := make([]map[string]any, 20)
	for index := range documents {
		documents[index] = map[string]any{
			"id": fmt.Sprintf("project_%d", index), "text": []string{strings.Repeat("x", 1<<20)},
		}
	}
	indexedBytes, err := worker.importSearchDocumentBatches(context.Background(), "projects", documents)
	if err != nil {
		t.Fatal(err)
	}
	if requestCount < 3 || maximumRequestBytes > searchImportByteBudget || indexedBytes < 20<<20 {
		t.Fatalf("requests=%d maximumBytes=%d indexedBytes=%d", requestCount, maximumRequestBytes, indexedBytes)
	}
}

func TestSearchStringArraysHaveDeterministicItemAndByteBudgets(t *testing.T) {
	values := make([]string, 100)
	for index := range values {
		values[index] = strings.Repeat("界", 300)
		values[index] += fmt.Sprintf("-%d", index)
	}
	compacted := compactStrings(values)
	totalBytes := 0
	for _, value := range compacted {
		totalBytes += len(value)
		if !utf8.ValidString(value) {
			t.Fatalf("budget truncation produced invalid UTF-8")
		}
	}
	if len(compacted) > searchDocumentArrayItemLimit || totalBytes > searchDocumentArrayByteLimit {
		t.Fatalf("items=%d bytes=%d", len(compacted), totalBytes)
	}
}
