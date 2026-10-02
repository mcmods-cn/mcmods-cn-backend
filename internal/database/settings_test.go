package database

import (
	"encoding/json"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestPersistedNATSConfigIsCompleteAuthority(t *testing.T) {
	stored := config.NATSConfig{
		Enabled: false, URL: "nats://stored:4222", Username: "", Password: "", Token: "",
		SubjectPrefix: "stored", Tasks: []config.NATSTaskConfig{}, OutboxEnabled: false, Realtime: false,
		JetStream: config.JetStreamConfig{
			Enabled: false, Stream: "STORED", MaxDeliver: 4,
			AckWait: 17 * time.Second, PublishTimeout: 3 * time.Second,
		},
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNATSConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Enabled || decoded.OutboxEnabled || decoded.Realtime || decoded.JetStream.Enabled {
		t.Fatalf("explicit false values were not preserved: %#v", decoded)
	}
	if decoded.Password != "" || decoded.Token != "" {
		t.Fatalf("explicitly empty credentials were repopulated: %#v", decoded)
	}
	if decoded.JetStream.AckWait != 17*time.Second || decoded.JetStream.PublishTimeout != 3*time.Second {
		t.Fatalf("JetStream durations did not survive persistence: %#v", decoded.JetStream)
	}
}
