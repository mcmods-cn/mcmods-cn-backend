package httpapi

import (
	"fmt"
	"strings"
	"testing"
)

func TestKeyMappingTranslationIDsResolveTheirOwningNamespace(t *testing.T) {
	for _, tc := range []struct{ id, namespace, resourcePath string }{
		{"create.keyinfo.rotate_menu", "create", "keyinfo.rotate_menu"},
		{"key.ae2.portable_fluid_cell", "ae2", "portable_fluid_cell"},
		{"key.mekanism.chest_mode", "mekanism", "chest_mode"},
	} {
		t.Run(tc.namespace, func(t *testing.T) {
			raw := []byte(fmt.Sprintf(`{"registry":"key_mappings","entries":[{"id":%q,"default_key":"key.keyboard.g"}]}`, tc.id))
			revisions := map[string]string{tc.namespace: "revision-" + tc.namespace}
			rows, err := prepareExportRegistryResources(nil, revisions, "registries/key_mappings.json", raw)
			if err != nil || len(rows) != 1 {
				t.Fatalf("rows=%d err=%v", len(rows), err)
			}
			batch := newModExportWriteBatch()
			if err = queueExportDocumentEntries(batch, nil, revisions, "registries/key_mappings.json", raw); err != nil || len(batch.catalogRows) != 1 {
				t.Fatalf("document rows=%d err=%v", len(batch.catalogRows), err)
			}
			for _, row := range []catalogResourceImportRow{rows[0], batch.catalogRows[0]} {
				if row.Namespace != tc.namespace || row.ResourcePath != tc.resourcePath || row.RawID != tc.id || row.CanonicalID != tc.namespace+":"+tc.resourcePath || row.RevisionID != "revision-"+tc.namespace {
					t.Fatalf("wrong ownership/identity: %+v", row)
				}
			}
			err = queueExportDocumentEntries(newModExportWriteBatch(), nil, map[string]string{"unrelated": "revision-unrelated"}, "registries/key_mappings.json", raw)
			if err == nil || !strings.Contains(err.Error(), "has no import revision") {
				t.Fatalf("unknown namespace accepted: %v", err)
			}
		})
	}
}
