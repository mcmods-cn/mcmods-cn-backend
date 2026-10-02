package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestActivityCleanupDoesNotConfirmBeforeAuditStatePersists(t *testing.T) {
	raw, err := os.ReadFile("activity_retention_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	executeBody := goFunctionBody(t, source, "executeActivityCleanup")
	pruneBody := goFunctionBody(t, source, "prune")
	for _, body := range []string{executeBody, pruneBody} {
		if strings.Contains(body, "_, _ =") && strings.Contains(body, "activity_cleanup_runs set status") {
			t.Fatal("cleanup final status persistence is still ignored")
		}
	}
	for _, required := range []string{
		"finalizeActivityCleanupRun",
		"ACTIVITY_CLEANUP_AUDIT_PENDING",
		"repairActivityCleanupRuns",
		"deleted_count=activity_cleanup_runs.deleted_count+",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("cleanup audit recovery is missing %q", required)
		}
	}
}
