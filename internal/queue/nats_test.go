package queue

import (
	"testing"

	"mcmods-cn-backend/internal/config"
)

func TestNormalizeConfigAddsDefaultTaskForLegacyConfig(t *testing.T) {
	cfg := NormalizeConfig(config.NATSConfig{SubjectPrefix: " mcmods "})
	if len(cfg.Tasks) != 1 || cfg.Tasks[0].Code != "ai" {
		t.Fatalf("expected default AI task, got %#v", cfg.Tasks)
	}
	if cfg.URL != "nats://127.0.0.1:4222" {
		t.Fatalf("unexpected default URL %q", cfg.URL)
	}
}

func TestNormalizeConfigPreservesExplicitEmptyTaskList(t *testing.T) {
	cfg := NormalizeConfig(config.NATSConfig{Tasks: []config.NATSTaskConfig{}})
	if cfg.Tasks == nil || len(cfg.Tasks) != 0 {
		t.Fatalf("expected explicit empty task list, got %#v", cfg.Tasks)
	}
}

func TestNormalizeConfigNormalizesTaskLimitsAndDuplicates(t *testing.T) {
	cfg := NormalizeConfig(config.NATSConfig{Tasks: []config.NATSTaskConfig{
		{Code: " AI ", Enabled: true, MaxConcurrent: 0, TimeoutSeconds: 0},
		{Code: "ai", Enabled: true, MaxConcurrent: 20, TimeoutSeconds: 20},
	}})
	if len(cfg.Tasks) != 1 {
		t.Fatalf("expected duplicate task codes to be removed, got %#v", cfg.Tasks)
	}
	task := cfg.Tasks[0]
	if task.Code != "ai" || task.Subject != "ai.tasks" || task.QueueGroup != "mcmods-ai-workers" {
		t.Fatalf("unexpected normalized task %#v", task)
	}
	if task.MaxConcurrent != 1 || task.TimeoutSeconds != 300 {
		t.Fatalf("unexpected normalized limits %#v", task)
	}
}
