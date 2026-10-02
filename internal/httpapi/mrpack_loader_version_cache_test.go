package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestFetchMinecraftSourceRevalidatesExpiredMetadata(t *testing.T) {
	resetMinecraftSourceCacheForTest()
	defer resetMinecraftSourceCacheForTest()
	originalClient := minecraftVersionHTTPClient
	defer func() { minecraftVersionHTTPClient = originalClient }()

	var requests atomic.Int64
	minecraftVersionHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestNumber := requests.Add(1)
		if requestNumber == 1 {
			response := loaderMetadataResponse(request, http.StatusOK, "metadata-v1")
			response.Header.Set("ETag", `"metadata-v1"`)
			response.Header.Set("Last-Modified", "Fri, 21 Aug 2026 01:02:03 GMT")
			return response, nil
		}
		if got := request.Header.Get("If-None-Match"); got != `"metadata-v1"` {
			t.Errorf("If-None-Match = %q", got)
		}
		if got := request.Header.Get("If-Modified-Since"); got != "Fri, 21 Aug 2026 01:02:03 GMT" {
			t.Errorf("If-Modified-Since = %q", got)
		}
		return loaderMetadataResponse(request, http.StatusNotModified, ""), nil
	})}

	const sourceURL = "https://metadata.example.test/forge.xml"
	first, err := fetchMinecraftSource(context.Background(), sourceURL)
	if err != nil || string(first) != "metadata-v1" {
		t.Fatalf("first fetch: got %q, %v", first, err)
	}
	minecraftSourceResponses.mu.Lock()
	entry := minecraftSourceResponses.entries[sourceURL]
	entry.freshUntil = time.Time{}
	minecraftSourceResponses.entries[sourceURL] = entry
	minecraftSourceResponses.mu.Unlock()
	second, err := fetchMinecraftSource(context.Background(), sourceURL)
	if err != nil || string(second) != "metadata-v1" {
		t.Fatalf("conditional fetch: got %q, %v", second, err)
	}
	third, err := fetchMinecraftSource(context.Background(), sourceURL)
	if err != nil || string(third) != "metadata-v1" {
		t.Fatalf("fresh fetch: got %q, %v", third, err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("request count = %d, want initial 200 plus one conditional 304", got)
	}
}

func TestFetchMinecraftSourceDoesNotCacheFailures(t *testing.T) {
	resetMinecraftSourceCacheForTest()
	defer resetMinecraftSourceCacheForTest()
	originalClient := minecraftVersionHTTPClient
	defer func() { minecraftVersionHTTPClient = originalClient }()

	var requests atomic.Int64
	minecraftVersionHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return loaderMetadataResponse(request, http.StatusServiceUnavailable, "unavailable"), nil
		}
		return loaderMetadataResponse(request, http.StatusOK, "recovered"), nil
	})}

	const sourceURL = "https://metadata.example.test/retry.xml"
	if _, err := fetchMinecraftSource(context.Background(), sourceURL); err == nil {
		t.Fatal("first failed response unexpectedly succeeded")
	}
	payload, err := fetchMinecraftSource(context.Background(), sourceURL)
	if err != nil || string(payload) != "recovered" {
		t.Fatalf("retry: got %q, %v", payload, err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("request count = %d, want failed attempt plus retry", got)
	}
}

func TestMinecraftSourceFetchesHaveGlobalConcurrencyBudget(t *testing.T) {
	resetMinecraftSourceCacheForTest()
	defer resetMinecraftSourceCacheForTest()
	originalClient := minecraftVersionHTTPClient
	defer func() { minecraftVersionHTTPClient = originalClient }()

	var active atomic.Int64
	var maximum atomic.Int64
	minecraftVersionHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
		return loaderMetadataResponse(request, http.StatusOK, "[]"), nil
	})}

	const callers = 12
	var wait sync.WaitGroup
	wait.Add(callers)
	errors := make(chan error, callers)
	for index := range callers {
		go func() {
			defer wait.Done()
			_, err := fetchMinecraftSource(context.Background(), fmt.Sprintf("https://metadata.example.test/%d", index))
			if err != nil {
				errors <- err
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	if got := maximum.Load(); got > maxMinecraftSourceFetches {
		t.Fatalf("maximum concurrent fetches = %d, budget = %d", got, maxMinecraftSourceFetches)
	}
	if got := maximum.Load(); got < 2 {
		t.Fatalf("test did not exercise concurrency: maximum = %d", got)
	}
}

func TestMinecraftSourceCacheHasEntryAndByteBudgets(t *testing.T) {
	cache := newMinecraftSourceResponseCache()
	for index := 0; index <= maxMinecraftSourceCacheItems; index++ {
		cache.store(fmt.Sprintf("https://metadata.example.test/small/%d", index), minecraftSourceCacheEntry{
			payload: []byte("x"), freshUntil: time.Now().Add(time.Hour),
		})
	}
	cache.mu.Lock()
	if got := len(cache.entries); got != maxMinecraftSourceCacheItems {
		cache.mu.Unlock()
		t.Fatalf("cache items = %d, want %d", got, maxMinecraftSourceCacheItems)
	}
	cache.mu.Unlock()

	cache.reset()
	maximumPayload := make([]byte, maxMinecraftSourceBytes)
	for index := 0; index < maxMinecraftSourceCacheBytes/maxMinecraftSourceBytes+1; index++ {
		cache.store(fmt.Sprintf("https://metadata.example.test/large/%d", index), minecraftSourceCacheEntry{
			payload: maximumPayload, freshUntil: time.Now().Add(time.Hour),
		})
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.totalBytes > maxMinecraftSourceCacheBytes {
		t.Fatalf("cache bytes = %d, budget = %d", cache.totalBytes, maxMinecraftSourceCacheBytes)
	}
}

func loaderMetadataResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}
