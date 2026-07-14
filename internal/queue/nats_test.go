package queue

import (
	"testing"

	"mcmods-cn-backend/internal/config"
)

func TestNormalizeConfigAddsDefaultTasks(t *testing.T) {
	cfg := NormalizeConfig(config.NATSConfig{SubjectPrefix: " mcmods "})
	if !hasRequiredTaskCodes(cfg.Tasks) {
		t.Fatalf("expected required system tasks, got %#v", cfg.Tasks)
	}
	if cfg.URL != "nats://127.0.0.1:4222" {
		t.Fatalf("unexpected default URL %q", cfg.URL)
	}
}

func TestNormalizeConfigRestoresRequiredTasksForExplicitEmptyList(t *testing.T) {
	cfg := NormalizeConfig(config.NATSConfig{Tasks: []config.NATSTaskConfig{}})
	if !hasRequiredTaskCodes(cfg.Tasks) {
		t.Fatalf("expected required system tasks, got %#v", cfg.Tasks)
	}
}

func TestNormalizeConfigNormalizesTaskLimitsAndDuplicates(t *testing.T) {
	cfg := NormalizeConfig(config.NATSConfig{Tasks: []config.NATSTaskConfig{
		{Code: " AI ", Enabled: true, MaxConcurrent: 0, TimeoutSeconds: 0},
		{Code: "ai", Enabled: true, MaxConcurrent: 20, TimeoutSeconds: 20},
	}})
	if !hasRequiredTaskCodes(cfg.Tasks) {
		t.Fatalf("expected duplicate task codes to be removed and required tasks restored, got %#v", cfg.Tasks)
	}
	task := cfg.Tasks[0]
	if task.Code != "ai" || task.Subject != "ai.tasks" || task.QueueGroup != "mcmods-ai-workers" {
		t.Fatalf("unexpected normalized task %#v", task)
	}
	if task.MaxConcurrent != 1 || task.TimeoutSeconds != 300 {
		t.Fatalf("unexpected normalized limits %#v", task)
	}
}

func hasRequiredTaskCodes(tasks []config.NATSTaskConfig) bool {
	required := map[string]bool{
		"ai": false, "notifications": false, "mod_export_import": false, "mod_metadata_import": false,
	}
	for _, task := range tasks {
		if _, exists := required[task.Code]; exists {
			required[task.Code] = true
		}
	}
	for _, found := range required {
		if !found {
			return false
		}
	}
	return true
}
