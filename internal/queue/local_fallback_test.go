package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestTaskSubscriptionRemainsAvailableToPostgresFallbackWithoutNATS(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := New(ctx, config.NATSConfig{Enabled: false, Tasks: []config.NATSTaskConfig{{
		Code: "mod_export_import", Enabled: true, Subject: "export.import.requested", QueueGroup: "test",
		MaxConcurrent: 1, TimeoutSeconds: 30,
	}}})
	defer client.Close()
	delivered := false
	err := client.SubscribeTask("mod_export_import", func(_ context.Context, raw []byte) error {
		delivered = string(raw) == `{"jobId":"job-1"}`
		return nil
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("offline subscription error = %v", err)
	}
	envelope, marshalErr := json.Marshal(EventEnvelope{EventID: "event-1", EventType: "mod.import.requested", SchemaVersion: 1,
		OccurredAt: time.Now().UTC(), AggregateType: "mod_import_job", AggregateID: "job-1", Payload: json.RawMessage(`{"jobId":"job-1"}`)})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if err = client.HandleLocally(ctx, "mod_export_import", "event-1", envelope); err != nil {
		t.Fatal(err)
	}
	if !delivered {
		t.Fatal("registered task was not delivered through the local outbox fallback")
	}
}
