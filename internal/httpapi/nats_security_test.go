package httpapi

import (
	"strings"
	"testing"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

func TestNATSConfigResponseRedactsEmbeddedServerCredentials(t *testing.T) {
	cfg := config.NATSConfig{URL: "nats://synthetic-user:synthetic-password@localhost:4222,nats://synthetic-token@127.0.0.1:4223?password=synthetic-query-secret#synthetic-fragment", Password: "synthetic-other-password", Token: "synthetic-other-token"}
	response := redactNATSConfig(cfg, queue.Status{})
	if response.URL != "nats://localhost:4222,nats://127.0.0.1:4223" {
		t.Fatalf("unexpected redacted server URL: %q", response.URL)
	}
	if !response.HasPassword || !response.HasToken {
		t.Fatal("credential presence indicators were lost")
	}
	if strings.Contains(response.URL, "synthetic-") {
		t.Fatal("NATS public configuration contains a secret")
	}
}
