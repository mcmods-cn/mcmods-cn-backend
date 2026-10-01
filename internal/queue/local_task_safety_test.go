package queue

import (
	"context"
	"errors"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/config"
)

func TestLocalOutboxHonorsDisabledTask(t *testing.T) {
	called := false
	client := &Client{
		cfg: NormalizeConfig(config.NATSConfig{Tasks: []config.NATSTaskConfig{{Code: "disabled", Enabled: false}}}),
		subscriptions: map[string]subscriptionDefinition{"disabled": {taskCode: "disabled", handler: func(context.Context, []byte) error {
			called = true
			return nil
		}}},
	}
	if err := client.HandleLocally(context.Background(), "disabled", "event", []byte(`{}`)); !errors.Is(err, ErrTaskDisabled) {
		t.Fatalf("disabled local task error=%v", err)
	}
	if called {
		t.Fatal("disabled task executed through local fallback")
	}
}

func TestQueueStatusDoesNotExposeServerCredentials(t *testing.T) {
	client := &Client{cfg: config.NATSConfig{URL: "nats://synthetic-user:synthetic-password@127.0.0.1:4222,nats://other:other-password@localhost:4223?token=synthetic-token"}, lastError: "provider message with synthetic-password"}
	status := client.Status()
	if strings.Contains(status.URL, "password") || strings.Contains(status.URL, "user") || strings.Contains(status.URL, "token") || strings.Contains(status.LastError, "synthetic-password") {
		t.Fatalf("queue status contains credentials: %+v", status)
	}
}

func TestQueueStatusDoesNotExposeMisplacedPathCredentials(t *testing.T) {
	for _, value := range []string{"nats://127.0.0.1:4222/synthetic-secret", "nats://127.0.0.1:4222/%73ynthetic-secret", "synthetic-secret"} {
		if result := RedactServerURLs(value); strings.Contains(result, "secret") {
			t.Fatalf("misplaced credential was exposed in status: %q", result)
		}
	}
}
