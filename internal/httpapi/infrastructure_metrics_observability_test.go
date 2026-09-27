package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestInfrastructureMetricsCannotReportDatabaseFailuresAsZero(t *testing.T) {
	raw, err := os.ReadFile("infrastructure_metrics.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	body := goFunctionBody(t, source, "infrastructureMetrics")
	if strings.Contains(body, "_ = s.db.QueryRow") {
		t.Fatal("infrastructure metrics still discards the durable metric query error")
	}
	for _, required := range []string{
		"loadInfrastructureDurableMetrics",
		"failed to load durable infrastructure metrics",
		"http.StatusInternalServerError",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("infrastructure metric failure boundary is missing %q", required)
		}
	}
}
