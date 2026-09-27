package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestOPS010PopularityQueuesHaveTerminalRetriesAndVisiblePersistenceErrors(t *testing.T) {
	worker, err := os.ReadFile("popularity_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(worker)
	for _, required := range []string{
		"popularityRefreshMaxAttempts",
		"status='pending'",
		"status='processing'",
		"status='failed'",
		"retryContentStatsTask(ctx, db, task, err)",
		"retryCommentHeatTask(ctx, db, task, err)",
		"return errors.Join",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("popularity retry state machine is missing %q", required)
		}
	}
	if strings.Contains(source, "_, _ = db.Exec") {
		t.Fatal("popularity worker still discards retry persistence errors")
	}
}
