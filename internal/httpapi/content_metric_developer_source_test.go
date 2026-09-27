package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestMetricEditorsUseOnlyTheEffectiveAccessSet(t *testing.T) {
	t.Parallel()
	payload, err := os.ReadFile("content_metrics_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ToLower(string(payload))
	if !strings.Contains(source, "from effective_project_access access") {
		t.Fatal("metric editor projection does not use the effective access authority")
	}
	for _, forbidden := range []string{"content_target_owner_id", "select min(access.user_id)"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("metric editor projection retains lossy scalar mapping %q", forbidden)
		}
	}
}
