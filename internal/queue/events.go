package queue

import (
	"context"
	"encoding/json"
	"time"
)

type EventEnvelope struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	SchemaVersion int             `json:"schema_version"`
	OccurredAt    time.Time       `json:"occurred_at"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	TraceID       string          `json:"trace_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

type eventIDContextKey struct{}

func WithEventID(ctx context.Context, eventID string) context.Context {
	return context.WithValue(ctx, eventIDContextKey{}, eventID)
}

func EventIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(eventIDContextKey{}).(string)
	return value
}

func UnwrapEvent(raw []byte) ([]byte, *EventEnvelope) {
	var envelope EventEnvelope
	if json.Unmarshal(raw, &envelope) == nil && envelope.EventID != "" && envelope.SchemaVersion > 0 && envelope.Payload != nil {
		return envelope.Payload, &envelope
	}
	return raw, nil
}
