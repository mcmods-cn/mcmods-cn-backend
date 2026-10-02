package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestBUG052RuntimeStartsAnAutomaticLogRetentionWorker(t *testing.T) {
	runtimeSource, err := os.ReadFile("../app/runtime.go")
	if err != nil {
		t.Fatal(err)
	}
	workerSource, err := os.ReadFile("log_retention_worker.go")
	if err != nil {
		t.Errorf("automatic log retention worker is absent: %v", err)
		return
	}
	retentionSource, err := os.ReadFile("log_handlers.go")
	if err != nil {
		t.Fatal(err)
	}

	runtimeContract := string(runtimeSource)
	if !strings.Contains(runtimeContract, "NewLogRetentionWorker(db).Start(ctx)") {
		t.Error("application runtime does not start the automatic log retention worker")
	}
	workerContract := string(workerSource)
	for _, required := range []string{
		"worker.prune(ctx)",
		"time.NewTicker(logRetentionInterval)",
		"pg_try_advisory_lock",
		"loadLogRetentionConfig",
		"logCleanupStatements",
	} {
		if !strings.Contains(workerContract, required) {
			t.Errorf("automatic log retention worker is missing %q", required)
		}
	}
	if !strings.Contains(string(retentionSource), "logs.retention") {
		t.Error("shared log retention config loader is missing its persisted setting key")
	}
}
