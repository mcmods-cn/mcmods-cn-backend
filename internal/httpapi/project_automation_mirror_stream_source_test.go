package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestProjectAutomationMirrorStreamsThroughBoundedGlobalSlots(t *testing.T) {
	raw, err := os.ReadFile("project_automation_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func downloadProviderFile")
	end := strings.Index(source, "func (worker *ProjectAutomationWorker) promoteCleanMirrors")
	if start < 0 || end <= start {
		t.Fatal("could not isolate provider mirror download")
	}
	download := source[start:end]
	if strings.Contains(download, "io.ReadAll") || strings.Contains(download, "[]byte, string, error") {
		t.Fatal("provider mirror still materializes the complete file in memory")
	}
	for _, required := range []string{
		"spoolProviderFile", "os.CreateTemp", "io.CopyBuffer", "projectAutomationMirrorBufferBytes",
		"acquireProjectAutomationMirrorSlot", "pg_try_advisory_lock", "projectAutomationMirrorGlobalConcurrency",
		"errors.Is(err, pgx.ErrNoRows)", "cleanupUnregisteredProjectMirror", "context.WithoutCancel(ctx)",
		"project-automation-registration-failed",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("streaming mirror boundary is missing %q", required)
		}
	}
}
