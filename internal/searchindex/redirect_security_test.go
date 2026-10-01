package searchindex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestTypesenseRedirectCannotForwardAPIKeyToAnotherOrigin(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded.Store(r.Header.Get("X-TYPESENSE-API-KEY") == "synthetic-key")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client := New(config.TypesenseConfig{Enabled: true, URL: source.URL, APIKey: "synthetic-key", Timeout: time.Second})
	if err := client.Health(context.Background()); err == nil || forwarded.Load() {
		t.Fatalf("cross-origin redirect accepted or credential forwarded: forwarded=%t", forwarded.Load())
	}
}

func TestTypesenseSameOriginRedirectRemainsSupported(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			http.Redirect(w, r, "/local-health", http.StatusTemporaryRedirect)
			return
		}
		if r.Header.Get("X-TYPESENSE-API-KEY") != "synthetic-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer source.Close()
	client := New(config.TypesenseConfig{Enabled: true, URL: source.URL, APIKey: "synthetic-key", Timeout: time.Second})
	if err := client.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}
