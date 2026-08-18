package database

import (
	"strings"
	"testing"
)

func TestEngagementExportSchemaDefinesCurrentContracts(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(strings.Join(engagementExportSchemaStatements(), "\n"))
	for _, required := range []string{
		"create table favorite_modpack_export_tasks",
		"check(pack_version_id <> minecraft_version)",
		"check(loader_type in ('neoforge','fabric','forge'))",
		"create table favorite_modpack_export_items",
		"check(result_type in ('exported','auto_dependency','skipped','failed'))",
		"create unique index uq_favorite_modpack_export_item_file",
		"create table sticker_packs",
		"code text not null unique",
		"unique(pack_id,code)",
		"check(mime_type in ('image/png','image/gif'))",
		"create table project_follows",
		"primary key(user_id,project_route_id)",
		"unique(project_route_id,publication_batch_id)",
		"create unique index uq_notifications_project_update_recipient",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("engagement/export schema is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"legacy",
		"backfill",
		"dual_read",
		"old_",
	} {
		if strings.Contains(definition, forbidden) {
			t.Fatalf("current development schema contains legacy marker %q", forbidden)
		}
	}
}

func TestEngagementPermissionsAreSeededOnce(t *testing.T) {
	t.Parallel()
	required := map[string]bool{
		"notification.template.manage":     false,
		"favorite.modpack_export":          false,
		"favorite.modpack_export.view_own": false,
		"sticker.view":                     false,
		"sticker.manage":                   false,
		"sticker.upload":                   false,
		"project.follow":                   false,
		"project.follow.view_own":          false,
	}
	seen := make(map[string]struct{}, len(seedPermissions))
	for _, permission := range seedPermissions {
		if _, duplicate := seen[permission.Code]; duplicate {
			t.Fatalf("permission %q is seeded more than once", permission.Code)
		}
		seen[permission.Code] = struct{}{}
		if _, wanted := required[permission.Code]; wanted {
			required[permission.Code] = true
		}
	}
	for code, found := range required {
		if !found {
			t.Fatalf("required permission %q is not seeded", code)
		}
	}
}
