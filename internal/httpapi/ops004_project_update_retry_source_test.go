package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestOPS004RetryOwnsAttemptIncrementAndPropagatesPersistenceErrors(t *testing.T) {
	source, err := os.ReadFile("project_update_notification_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	worker := string(source)
	for _, required := range []string{
		"projectUpdateNotificationMaxAttempts",
		"attempt_count=attempt_count+1",
		"attempt_count+1>=$3",
		"return err",
	} {
		if !strings.Contains(worker, required) {
			t.Errorf("retry state machine is missing %q", required)
		}
	}
	if strings.Contains(worker, "_, _ = worker.db.Exec") {
		t.Fatal("project update retry still discards its own persistence failure")
	}
	processStart := strings.Index(worker, "func (worker *ProjectUpdateNotificationWorker) process(")
	retryStart := strings.Index(worker, "func (worker *ProjectUpdateNotificationWorker) retry(")
	if processStart < 0 || retryStart <= processStart {
		t.Fatal("could not isolate project update process and retry state machines")
	}
	if strings.Contains(worker[processStart:retryStart], "attempt_count=attempt_count+1") {
		t.Fatal("successful recipient batches consume the failure retry budget")
	}
}
