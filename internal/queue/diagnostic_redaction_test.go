package queue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/config"
)

func TestQueueStatusNeverReturnsEmbeddedOrExplicitCredentials(t *testing.T) {
	for _, scenario := range []struct {
		name, endpoint, safe string
	}{
		{"single", "nats://fixture-user:synthetic-password@127.0.0.1:4222", "nats://127.0.0.1:4222"},
		{"token", "nats://synthetic-token@127.0.0.1:4222", "nats://127.0.0.1:4222"},
		{"multiple", "nats://fixture-user:synthetic-password@127.0.0.1:4222,nats://synthetic-token@localhost:4223", "nats://127.0.0.1:4222,nats://localhost:4223"},
		{"invalid", "nats://fixture-user:synthetic-password@invalid%host:4222", "[invalid NATS endpoint]"},
		{"query", "nats://127.0.0.1:4222?token=synthetic-token", "nats://127.0.0.1:4222"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			client := New(ctx, config.NATSConfig{URL: scenario.endpoint, Password: "synthetic-password", Token: "synthetic-token"})
			defer client.Close()
			client.setLastError(errors.New("parse " + scenario.endpoint + ": synthetic-password synthetic-token"))
			status := client.Status()
			if status.URL != scenario.safe {
				t.Fatal("status did not retain the expected credential-free endpoint")
			}
			raw, err := json.Marshal(status)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"synthetic-password", "synthetic-token"} {
				if strings.Contains(string(raw), secret) {
					t.Fatal("serialized queue status includes a credential")
				}
			}
		})
	}
}
