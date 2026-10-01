package queue

import (
	"testing"

	"mcmods-cn-backend/internal/config"
)

func TestFavoriteExportTaskExistsAndHonorsExplicitDisable(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		var tasks []config.NATSTaskConfig
		if explicit {
			tasks = []config.NATSTaskConfig{{Code: "favorite_modpack_export", Enabled: false}}
		}
		cfg := NormalizeConfig(config.NATSConfig{Tasks: tasks})
		var found bool
		for _, task := range cfg.Tasks {
			if task.Code != "favorite_modpack_export" {
				continue
			}
			found = true
			if task.Enabled == explicit {
				t.Fatalf("favorite export enabled=%t explicit-disable=%t", task.Enabled, explicit)
			}
			if !explicit && (task.Subject != "favorite.modpack_export.requested" || task.MaxConcurrent < 1 || task.TimeoutSeconds < 1) {
				t.Fatal("favorite export defaults cannot be dispatched")
			}
		}
		if !found {
			t.Fatal("favorite export is persisted but has no dispatchable task configuration")
		}
	}
}
