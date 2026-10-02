package queue

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestEventEnvelopeRoundTrip(t *testing.T) {
	payload := json.RawMessage(`{"value":7}`)
	envelope := EventEnvelope{EventID: "event-1", EventType: "test.created", SchemaVersion: 1, OccurredAt: time.Now().UTC(), AggregateType: "test", AggregateID: "42", Payload: payload}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	unwrapped, decoded, err := UnwrapEvent(raw)
	if err != nil || decoded == nil || decoded.EventID != envelope.EventID || string(unwrapped) != string(payload) {
		t.Fatalf("failed envelope round trip: %+v %s %v", decoded, unwrapped, err)
	}
}

func TestRawCoreMessageIsRejected(t *testing.T) {
	raw := []byte(`{"legacy":true}`)
	if payload, envelope, err := UnwrapEvent(raw); err == nil || payload != nil || envelope != nil {
		t.Fatalf("legacy payload was accepted: payload=%s envelope=%+v err=%v", payload, envelope, err)
	}
}

func TestEventEnvelopeRequiresCompleteVersionOneIdentity(t *testing.T) {
	valid := EventEnvelope{EventID: "event-1", EventType: "test.created", SchemaVersion: 1, OccurredAt: time.Now().UTC(), AggregateType: "test", AggregateID: "42", Payload: json.RawMessage(`{}`)}
	tests := []struct {
		name   string
		mutate func(*EventEnvelope)
	}{
		{"event ID", func(value *EventEnvelope) { value.EventID = "" }},
		{"event type", func(value *EventEnvelope) { value.EventType = "" }},
		{"schema version", func(value *EventEnvelope) { value.SchemaVersion = 2 }},
		{"occurred at", func(value *EventEnvelope) { value.OccurredAt = time.Time{} }},
		{"aggregate type", func(value *EventEnvelope) { value.AggregateType = "" }},
		{"aggregate ID", func(value *EventEnvelope) { value.AggregateID = "" }},
		{"payload", func(value *EventEnvelope) { value.Payload = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			raw, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = UnwrapEvent(raw); !errors.Is(err, ErrInvalidEventEnvelope) {
				t.Fatalf("invalid %s envelope error = %v", test.name, err)
			}
		})
	}
}

func TestLocalTaskHandlerRejectsBarePayload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := New(ctx, config.NATSConfig{})
	called := false
	_ = client.SubscribeTask("ai", func(context.Context, []byte) error {
		called = true
		return nil
	})
	if err := client.HandleLocally(ctx, "ai", "legacy-header", []byte(`{"legacy":true}`)); !errors.Is(err, ErrInvalidEventEnvelope) {
		t.Fatalf("bare local task error = %v", err)
	}
	if called {
		t.Fatal("bare task reached the application handler")
	}
}

func TestTaskProducersUseOnlyEventEnvelopes(t *testing.T) {
	err := filepath.WalkDir("../httpapi", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(raw), ".PublishTask(") {
			t.Fatalf("task producer %s still emits a bare Core NATS payload", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	natsSource, err := os.ReadFile("nats.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(natsSource), "func (c *Client) PublishTask(") {
		t.Fatal("queue client still exposes the bare task publisher")
	}
}

func TestRandomEventIDsAreUnique(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for index := 0; index < 1000; index++ {
		id := randomEventID()
		if seen[id] {
			t.Fatalf("duplicate event ID %q", id)
		}
		seen[id] = true
	}
}
