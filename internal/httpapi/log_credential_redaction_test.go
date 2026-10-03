package httpapi

import (
	"strings"
	"testing"
)

func TestOCT02LogRedactionMasksQuotedSecretsAndNATSTokens(t *testing.T) {
	for _, input := range []string{`{"password":"synthetic-json-secret","message":"ordinary"}`, `{'client_secret':'synthetic-single-secret'}`, `nats://synthetic-nats-token@localhost:4222`, `nats://synthetic-user:synthetic-pass@localhost:4222`, `{"access_token":"synthetic-secret with spaces"}`} {
		output, counts := redactLogText(input)
		if strings.Contains(output, "synthetic-") {
			t.Fatalf("synthetic credential remained visible: %q", output)
		}
		if len(counts) == 0 || !strings.Contains(output, "❄") {
			t.Fatal("missing redaction evidence")
		}
	}
	output, _ := redactLogText(`{"message":"ordinary","version":"1.2.3"}`)
	if output != `{"message":"ordinary","version":"1.2.3"}` {
		t.Fatal("ordinary log fields changed")
	}
}
