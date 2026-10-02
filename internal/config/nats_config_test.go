package config

import "testing"

func TestJetStreamDurableDeliveryIsEnabledByDefault(t *testing.T) {
	// A present non-boolean value prevents a developer .env file from
	// influencing the fallback contract under test.
	t.Setenv("NATS_JETSTREAM_ENABLED", "use-default")
	if cfg := Load(); !cfg.NATS.JetStream.Enabled {
		t.Fatal("fresh deployments must default to JetStream durable delivery")
	}
}
