package database

import (
	"strings"
	"testing"
)

func TestFavoriteModpackExportHistoryHasStatusAndCursorIndexes(t *testing.T) {
	source := strings.ToLower(strings.Join(schemaInstallationStatements(), "\n"))
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
