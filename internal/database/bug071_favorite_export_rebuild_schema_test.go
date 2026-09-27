package database

import (
	"strings"
	"testing"
)

func TestFavoriteExportRebuildPreviewsSupportDetachedSourcesInGeneration155(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("schema generation = %d, want 167", schemaGeneration)
	}
	schema := strings.Join(engagementExportSchemaStatements(), "\n")
	required := []string{
		"collection_id bigint references favorite_collections(id) on delete set null",
		"collection_public_id_snapshot text not null",
		"source_mode text not null default 'current_collection'",
		"check(source_mode in ('current_collection','original_snapshot'))",
	}
	for _, fragment := range required {
		if !strings.Contains(schema, fragment) {
			t.Fatalf("favorite export rebuild schema is missing %q", fragment)
		}
	}
	if strings.Contains(schema, "favorite_modpack_export_previews (\n\t\t\tid bigserial primary key,\n\t\t\tpublic_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),\n\t\t\towner_user_id bigint not null references users(id) on delete cascade,\n\t\t\tcollection_id bigint not null") {
		t.Fatal("rebuild previews still require a live source collection")
	}
}
