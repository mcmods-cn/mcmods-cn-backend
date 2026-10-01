package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMinecraftLoaderSourceFailuresAndFallbackAudit(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/unavailable":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/invalid":
			_, _ = w.Write([]byte(`{"broken":`))
		case "/empty":
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`["1.20.1"]`))
		}
	}))
	defer source.Close()
	for _, path := range []string{"/unavailable", "/invalid", "/empty"} {
		t.Run(path, func(t *testing.T) {
			versions, url, fallback, err := fetchMinecraftVersionAttempts(context.Background(), []minecraftSourceAttempt{
				{url: source.URL + path, decode: decodeStringArray}, {url: source.URL + "/valid", decode: decodeStringArray},
			})
			if err != nil || !fallback || url != source.URL+"/valid" || len(versions) != 1 || versions[0] != "1.20.1" {
				t.Fatalf("fallback failed: versions=%v url=%q fallback=%v error=%v", versions, url, fallback, err)
			}
		})
	}
	t.Run("all failures remain failures", func(t *testing.T) {
		_, _, _, err := fetchMinecraftVersionAttempts(context.Background(), []minecraftSourceAttempt{{url: source.URL + "/invalid", decode: decodeStringArray}})
		if err == nil {
			t.Fatal("invalid source succeeded")
		}
	})
	t.Run("oversized input is bounded", func(t *testing.T) {
		large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", maxMinecraftSourceBytes+1)))
		}))
		defer large.Close()
		if _, err := fetchMinecraftSource(context.Background(), large.URL); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("oversized source accepted: %v", err)
		}
	})
	t.Run("timeout cancels the local request", func(t *testing.T) {
		slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer slow.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if _, err := fetchMinecraftSource(ctx, slow.URL); err == nil {
			t.Fatal("cancelled request succeeded")
		}
	})
}
