package database

import (
	"strings"
	"testing"
)

func TestSkinTextureBlobsTrackAndCalibrateActiveReferences(t *testing.T) {
	t.Parallel()
	schema := strings.ToLower(strings.Join(skinSchemaStatements(), "\n"))
	for _, required := range []string{
		"active_reference_count bigint not null default 0",
		"idx_skin_texture_blobs_unreferenced",
		"where active_reference_count=0",
		"maintain_skin_texture_blob_reference_count",
		"trg_skin_assets_blob_reference_count",
		"rebuild_skin_texture_blob_reference_counts",
		"idx_skin_assets_active_blob",
		"where status='active'",
	} {
		if !strings.Contains(schema, required) {
			t.Errorf("skin texture reference schema is missing %q", required)
		}
	}
	if schemaGeneration < 155 {
		t.Fatalf("skin texture reference facts require a new schema generation, got %d", schemaGeneration)
	}
}
