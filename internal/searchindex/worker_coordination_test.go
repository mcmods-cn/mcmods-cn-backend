package searchindex

import (
	"os"
	"strings"
	"testing"
)

func TestSearchWorkerCoordinatesRebuildAndQueueDrainAcrossInstances(t *testing.T) {
	source, err := os.ReadFile("worker.go")
	if err != nil {
		t.Fatal(err)
	}
	contract := strings.ToLower(string(source))
	for _, required := range []string{
		"pg_advisory_lock(hashtext($1))",
		"pg_advisory_lock_shared(hashtext($1))",
		"withsearchprojectionlease(ctx, false",
		"withsearchprojectionlease(ctx, true",
		"leasedworker.projectionscurrent(ctx)",
		"leasedworker.db = connection",
	} {
		if !strings.Contains(contract, required) {
			t.Errorf("cross-instance search coordination is missing %q", required)
		}
	}
}
