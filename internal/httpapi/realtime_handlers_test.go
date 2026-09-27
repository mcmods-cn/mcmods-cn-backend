package httpapi

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRealtimeSSEHasLayeredBudgetsAndFiniteLifetime(t *testing.T) {
	if maxRealtimeConnections > 4096 || maxRealtimeConnectionsPerUser > 4 || maxRealtimeConnectionsPerSession > 2 {
		t.Fatalf("unsafe realtime limits: total=%d user=%d session=%d", maxRealtimeConnections, maxRealtimeConnectionsPerUser, maxRealtimeConnectionsPerSession)
	}
	if maxRealtimeConnectionLifetime <= 0 || maxRealtimeConnectionLifetime > time.Hour {
		t.Fatalf("unsafe realtime lifetime: %s", maxRealtimeConnectionLifetime)
	}
	source, err := os.ReadFile("realtime_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{"AcquireRealtimeLease", "lease.Refresh", "maxRealtimeConnectionLifetime", "REALTIME_CONNECTION_LIMIT"} {
		if !strings.Contains(text, required) {
			t.Fatalf("SSE handler is missing %q", required)
		}
	}
}
