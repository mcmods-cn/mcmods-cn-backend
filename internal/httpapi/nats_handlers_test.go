package httpapi

import (
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

func TestNATSUpdateRequiresCompleteReliabilityConfig(t *testing.T) {
	request := completeNATSUpdateRequest()
	request.OutboxEnabled = nil
	if cfg, err := request.config(config.NATSConfig{}); err != nil || !cfg.OutboxEnabled {
		t.Fatalf("omitting the legacy field changed mandatory outbox delivery: enabled=%t err=%v", cfg.OutboxEnabled, err)
	}
	request = completeNATSUpdateRequest()
	request.JetStream.PublishTimeoutSeconds = nil
	if _, err := request.config(config.NATSConfig{}); err == nil {
		t.Fatal("expected an incomplete JetStream object to reject the complete PUT")
	}
}

func TestNATSUpdatePreservesOrExplicitlyClearsSecrets(t *testing.T) {
	request := completeNATSUpdateRequest()
	empty := ""
	request.Password = &empty
	request.ClearToken = true
	current := config.NATSConfig{Password: "saved-password", Token: "saved-token"}
	next, err := request.config(current)
	if err != nil {
		t.Fatal(err)
	}
	if next.Password != "saved-password" {
		t.Fatalf("blank replacement should retain the password, got %q", next.Password)
	}
	if next.Token != "" {
		t.Fatalf("clearToken should remove the token, got %q", next.Token)
	}

	replacement := "replacement"
	request.Token = &replacement
	if _, err = request.config(current); err == nil {
		t.Fatal("expected replacement plus clearToken to be rejected")
	}
}

func TestNATSResponseIncludesAllReliabilityFields(t *testing.T) {
	cfg := queue.NormalizeConfig(config.NATSConfig{
		OutboxEnabled: true,
		Realtime:      true,
		JetStream: config.JetStreamConfig{
			Enabled: true, Stream: "AUDIT", MaxDeliver: 9,
			AckWait: 45 * time.Second, PublishTimeout: 4 * time.Second,
		},
	})
	response := redactNATSConfig(cfg, queue.Status{})
	if !response.OutboxEnabled || !response.Realtime || !response.JetStream.Enabled {
		t.Fatalf("reliability fields missing from response: %#v", response)
	}
	if response.JetStream.AckWaitSeconds != 45 || response.JetStream.PublishTimeoutSeconds != 4 {
		t.Fatalf("unexpected JetStream duration DTO: %#v", response.JetStream)
	}
}

func completeNATSUpdateRequest() natsConfigUpdateRequest {
	tasks := []config.NATSTaskConfig{{Code: "ai", Enabled: true, Subject: "ai.tasks", QueueGroup: "ai", MaxConcurrent: 2, TimeoutSeconds: 30}}
	return natsConfigUpdateRequest{
		Enabled: natsPtr(false), URL: natsPtr("nats://127.0.0.1:4222"), Username: natsPtr(""),
		SubjectPrefix: natsPtr("mcmods"), Tasks: &tasks, OutboxEnabled: natsPtr(false), Realtime: natsPtr(false),
		JetStream: &natsJetStreamUpdateRequest{
			Enabled: natsPtr(false), Stream: natsPtr("MCMODS_TASKS"), MaxDeliver: natsPtr(8),
			AckWaitSeconds: natsPtr(300), PublishTimeoutSeconds: natsPtr(5),
		},
	}
}

func natsPtr[T any](value T) *T { return &value }

func TestNATSNewStreamBudgetIsPreservedForOlderClients(t *testing.T) {
	request := completeNATSUpdateRequest()
	current := config.NATSConfig{JetStream: config.JetStreamConfig{NewStreamMaxBytes: 64 << 20}}
	next, err := request.config(current)
	if err != nil || next.JetStream.NewStreamMaxBytes != current.JetStream.NewStreamMaxBytes {
		t.Fatalf("old PUT lost creation-only budget: %d/%v", next.JetStream.NewStreamMaxBytes, err)
	}
	request.JetStream.NewStreamMaxBytes = natsPtr(int64(128 << 20))
	next, err = request.config(current)
	if err != nil || redactNATSConfig(next, queue.Status{}).JetStream.NewStreamMaxBytes != 128<<20 {
		t.Fatalf("new stream budget did not round-trip: %v", err)
	}
	request.JetStream.NewStreamMaxBytes = natsPtr(int64(0))
	if _, err = request.config(current); err == nil {
		t.Fatal("accepted an unlimited new stream budget")
	}
}
