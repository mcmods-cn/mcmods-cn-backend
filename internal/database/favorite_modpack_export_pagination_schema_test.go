package database

import (
	"os"
	"strings"
	"testing"
)

func TestFavoriteModpackExportHistoryHasStatusAndCursorIndexes(t *testing.T) {
	migrator, err := os.ReadFile("migrator.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(migrator), "schemaGeneration = 168") {
		t.Fatal("schema generation is not 168")
	}
	payload, err := os.ReadFile("engagement_export_schema.go")
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ToLower(string(payload))
	for _, required := range []string{
		"idx_favorite_modpack_export_tasks_owner_created",
		"(owner_user_id,created_at desc,id desc)",
		"idx_favorite_modpack_export_tasks_owner_status_created",
		"(owner_user_id,status,created_at desc,id desc)",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("export history schema is missing %q", required)
		}
	}
}
