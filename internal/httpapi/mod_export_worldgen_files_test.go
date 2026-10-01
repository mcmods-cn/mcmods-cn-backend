package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWorldgenDataFilesUseResourceIdentityNotAssetPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		file map[string]any
	}{
		{"exporter_v1", map[string]any{"kind": "dimension_type", "resource_id": "minecraft:dimension_type/overworld.json", "object_id": "minecraft:overworld", "source_mod_id": "minecraft", "path": "data/minecraft/dimension_type/overworld.json", "size_bytes": float64(569)}},
		{"path_only", map[string]any{"path": "data/minecraft/dimension_type/overworld.json"}},
		{"explicit_id", map[string]any{"id": "minecraft:dimension_type/overworld.json", "namespace": "minecraft"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"files": []any{tc.file}})
			if err != nil {
				t.Fatal(err)
			}
			batch := newModExportWriteBatch()
			err = queueExportDocumentEntries(batch, nil, map[string]string{"minecraft": "revision-minecraft", "example": "revision-example"}, "worldgen/data_files.json", raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(batch.catalogRows) != 1 {
				t.Fatalf("rows=%d, want one; file must not be silently omitted", len(batch.catalogRows))
			}
			row := batch.catalogRows[0]
			if row.RevisionID != "revision-minecraft" || row.Namespace != "minecraft" || row.ResourcePath != "dimension_type/overworld.json" || row.CanonicalID != "minecraft:dimension_type/overworld.json" || row.RawID != row.CanonicalID {
				t.Fatalf("incorrect file identity or revision: %+v", row)
			}
			if row.Registry != "worldgen_data" {
				t.Fatalf("registry=%q", row.Registry)
			}
			if tc.name == "exporter_v1" && !strings.Contains(row.Data, `"object_id":"minecraft:overworld"`) {
				t.Fatalf("source metadata lost: %s", row.Data)
			}
		})
	}
}

func TestWorldgenDataFilesRejectUnknownConflictingOrMalformedIdentity(t *testing.T) {
	files := []map[string]any{
		{"resource_id": "missing:dimension_type/overworld.json", "path": "data/missing/dimension_type/overworld.json"},
		{"resource_id": "minecraft:dimension_type/overworld.json", "namespace": "example"},
		{"resource_id": "minecraft:dimension_type/overworld.json", "path": "data/example/dimension_type/overworld.json"},
		{"resource_id": "minecraft:dimension_type/overworld.json", "id": "example:dimension_type/overworld.json"},
		{"resource_id": "minecraft:"},
		{"resource_id": "minecraft:../overworld.json"},
		{"path": "data/minecraft/../overworld.json"},
		{"path": "data//overworld.json"},
		{"path": "assets/minecraft/dimension_type/overworld.json"},
		{"resource_id": "dimension_type/overworld.json"},
		{},
	}
	for index, file := range files {
		for _, revisions := range []map[string]string{{"minecraft": "revision-minecraft"}, {"minecraft": "revision-minecraft", "example": "revision-example"}} {
			raw, err := json.Marshal(map[string]any{"files": []any{file}})
			if err != nil {
				t.Fatal(err)
			}
			batch := newModExportWriteBatch()
			err = queueExportDocumentEntries(batch, nil, revisions, "worldgen/data_files.json", raw)
			if err == nil || !strings.Contains(err.Error(), "worldgen/data_files.json") {
				t.Errorf("case %d with %d revisions: invalid entry silently accepted: rows=%d err=%v", index, len(revisions), len(batch.catalogRows), err)
			}
			if len(batch.catalogRows) != 0 {
				t.Errorf("case %d queued an invalid file before rejecting it", index)
			}
		}
	}
}

func TestWorldgenDataFilesRejectInvalidContainerShape(t *testing.T) {
	for _, raw := range []string{`{}`, `{"files":null}`, `{"files":{}}`, `{"files":[null]}`, `{"files":["data/minecraft/a.json"]}`} {
		if err := queueExportDocumentEntries(newModExportWriteBatch(), nil, map[string]string{"minecraft": "revision"}, "worldgen/data_files.json", []byte(raw)); err == nil {
			t.Errorf("malformed materialized file container silently accepted: %s", raw)
		}
	}
}
