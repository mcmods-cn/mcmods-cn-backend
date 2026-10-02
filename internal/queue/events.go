package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidEventEnvelope = errors.New("invalid event envelope")

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

func UnwrapEvent(raw []byte) ([]byte, *EventEnvelope, error) {
	var envelope EventEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalidEventEnvelope, err)
	}
	if strings.TrimSpace(envelope.EventID) == "" || strings.TrimSpace(envelope.EventType) == "" || envelope.SchemaVersion != 1 ||
		envelope.OccurredAt.IsZero() || strings.TrimSpace(envelope.AggregateType) == "" || strings.TrimSpace(envelope.AggregateID) == "" ||
		len(envelope.Payload) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Payload), []byte("null")) {
		return nil, nil, ErrInvalidEventEnvelope
	}
	return envelope.Payload, &envelope, nil
}
