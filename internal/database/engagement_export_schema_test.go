package database

import (
	"strings"
	"testing"
)

func TestEngagementExportSchemaDefinesCurrentContracts(t *testing.T) {
	t.Parallel()
	if schemaGeneration != 168 {
		t.Fatalf("unexpected schema generation %d", schemaGeneration)
	}
	definition := strings.ToLower(strings.Join(engagementExportSchemaStatements(), "\n"))
	for _, required := range []string{
		"create table favorite_modpack_export_previews",
		"preview_snapshot jsonb not null",
		"content_hash text not null",
		"consumed_at timestamptz",
		"create index idx_favorite_modpack_export_previews_owner_expiry",
		"create table favorite_modpack_export_tasks",
		"collection_id bigint references favorite_collections(id) on delete set null",
		"collection_public_id_snapshot text not null",
		"create index idx_favorite_modpack_export_tasks_collection",
		"create unique index uq_favorite_modpack_export_tasks_result_file",
		"create index idx_oss_files_favorite_export_orphan_recovery",
		"check(pack_version_id <> minecraft_version)",
		"check(loader_type in ('neoforge','fabric','forge'))",
		"create table favorite_modpack_export_items",
		"check(result_type in ('exported','auto_dependency','skipped','failed'))",
		"create unique index uq_favorite_modpack_export_item_file",
		"create table minecraft_loader_artifact_versions",
		"primary key(catalog_hash,minecraft_version,loader_type)",
		"source_url text not null",
		"observed_at timestamptz not null",
		"check(loader_type in ('neoforge','fabric','forge'))",
		"create table sticker_packs",
		"code text not null unique",
		"unique(pack_id,code)",
		"constraint uq_stickers_image_file unique(image_file_id)",
		"check(mime_type in ('image/png','image/gif'))",
		"create table project_follows",
		"primary key(user_id,project_route_id)",
		"create index idx_project_follows_user_created on project_follows(user_id,created_at desc,project_route_id)",
		"revision_id bigint references content_revisions(id) on delete restrict",
		"unique(project_route_id,publication_batch_id)",
		"create unique index uq_notifications_project_update_recipient",
		"create index idx_notifications_project_update_event",
		"on notifications(project_update_event_id) where project_update_event_id is not null",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("engagement/export schema is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"report_snapshot",
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

func TestStickerReferenceSchemaDefinesIndexedTransactionalProjection(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(strings.Join(stickerReferenceSchemaStatements(), "\n"))
	for _, required := range []string{
		"create table sticker_content_references",
		"primary key(source_type,source_key,source_field,pack_code,sticker_code)",
		"create index idx_sticker_content_references_token on sticker_content_references(pack_code,sticker_code)",
		"create or replace function sync_sticker_content_references()",
		"pg_advisory_xact_lock(hashtext('sticker-reference')",
		"regexp_matches",
		"column_name like '%\\_markdown' escape '\\'",
		"(column_info.table_name='comments' and column_info.column_name='body')",
		"(column_info.table_name='content_revisions' and column_info.column_name='snapshot')",
		"after insert on",
		"after update of",
		"after delete on",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("sticker reference schema is missing %q", required)
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
