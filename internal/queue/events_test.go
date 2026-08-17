package queue

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventEnvelopeRoundTrip(t *testing.T) {
	payload := json.RawMessage(`{"value":7}`)
	envelope := EventEnvelope{EventID: "event-1", EventType: "test.created", SchemaVersion: 1, OccurredAt: time.Now().UTC(), AggregateType: "test", AggregateID: "42", Payload: payload}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	unwrapped, decoded := UnwrapEvent(raw)
	if decoded == nil || decoded.EventID != envelope.EventID || string(unwrapped) != string(payload) {
		t.Fatalf("failed envelope round trip: %+v %s", decoded, unwrapped)
	}
}

func TestRawCoreMessageRemainsCompatible(t *testing.T) {
	raw := []byte(`{"legacy":true}`)
	unwrapped, envelope := UnwrapEvent(raw)
	if envelope != nil || string(unwrapped) != string(raw) {
		t.Fatal("legacy payload was not preserved")
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
