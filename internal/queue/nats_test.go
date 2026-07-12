package queue

import (
	"testing"

	"mcmods-cn-backend/internal/config"
)

func TestNormalizeConfigAddsDefaultTaskForLegacyConfig(t *testing.T) {
	cfg := NormalizeConfig(config.NATSConfig{SubjectPrefix: " mcmods "})
	if len(cfg.Tasks) != 3 || cfg.Tasks[0].Code != "ai" || cfg.Tasks[1].Code != "notifications" || cfg.Tasks[2].Code != "mod_export_import" {
		t.Fatalf("expected required system tasks, got %#v", cfg.Tasks)
	}
	if cfg.URL != "nats://127.0.0.1:4222" {
		t.Fatalf("unexpected default URL %q", cfg.URL)
	}
}

func TestNormalizeConfigRestoresRequiredTasksForExplicitEmptyList(t *testing.T) {
	cfg := NormalizeConfig(config.NATSConfig{Tasks: []config.NATSTaskConfig{}})
	if len(cfg.Tasks) != 3 {
		t.Fatalf("expected required system tasks, got %#v", cfg.Tasks)
	}
}

func TestNormalizeConfigNormalizesTaskLimitsAndDuplicates(t *testing.T) {
	cfg := NormalizeConfig(config.NATSConfig{Tasks: []config.NATSTaskConfig{
		{Code: " AI ", Enabled: true, MaxConcurrent: 0, TimeoutSeconds: 0},
		{Code: "ai", Enabled: true, MaxConcurrent: 20, TimeoutSeconds: 20},
	}})
	if len(cfg.Tasks) != 3 {
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
