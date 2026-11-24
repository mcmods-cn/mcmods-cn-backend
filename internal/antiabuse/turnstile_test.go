package antiabuse

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"mcmods-cn-backend/internal/config"
)

func TestTurnstileAcceptsOnlyConfiguredEndpointResponse(t *testing.T) {
	var visits atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/verify":
			_, _ = io.WriteString(w, `{"success":true}`)
		case "/moved":
			http.Redirect(w, r, "/replacement", http.StatusTemporaryRedirect)
		case "/replacement":
			visits.Add(1)
			_, _ = io.WriteString(w, `{"success":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	service := &Service{cfg: config.AntiAbuseConfig{TurnstileSecretKey: "test-only", TurnstileVerifyURL: provider.URL + "/verify"}}
	if ok, err := service.verifyTurnstile(context.Background(), "test-response", ""); err != nil || !ok {
		t.Fatalf("direct verification ok=%v err=%v", ok, err)
	}
	service.cfg.TurnstileVerifyURL = provider.URL + "/moved"
	if ok, err := service.verifyTurnstile(context.Background(), "test-response", ""); err == nil || ok || visits.Load() != 0 {
		t.Fatalf("unexpected redirect result ok=%v err=%v additional calls=%d", ok, err, visits.Load())
	}
}
