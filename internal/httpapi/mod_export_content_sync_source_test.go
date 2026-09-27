package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestImportedContentSyncLocksVersionBeforeOrdinalAllocation(t *testing.T) {
	raw, err := os.ReadFile("mod_export_content_sync.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	lockCall := strings.Index(source, "lockImportedContentVersionTx(ctx, tx, versionID)")
	firstWrite := strings.Index(source, "insert into mod_resource_bindings")
	if lockCall < 0 || firstWrite <= lockCall {
		t.Fatal("content version lock must precede every imported projection and ordinal allocation")
	}
	for _, required := range []string{"pg_advisory_xact_lock", "mod-content-import:", "status='active' for update"} {
		if !strings.Contains(source, required) {
			t.Errorf("content version lock is missing %q", required)
		}
	}
}

func TestImportedPlacementConflictIsNarrowlyIdempotent(t *testing.T) {
	raw, err := os.ReadFile("mod_export_content_sync.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.LastIndex(source, "insert into mod_content_section_resources")
	if start < 0 {
		t.Fatal("import placement statement inventory changed")
	}
	end := strings.Index(source[start:], "_, err := tx.Exec(ctx, placementSQL")
	if end < 0 {
		t.Fatal("import placement statement inventory changed")
	}
	statement := source[start : start+end]
	if !strings.Contains(statement, "on conflict(version_id,resource_id) do nothing") {
		t.Fatal("import placement does not name the sole idempotent resource conflict")
	}
	if strings.Contains(statement, "on conflict do nothing") {
		t.Fatal("import placement still silently ignores ordinal or identity collisions")
	}
}

func TestImportedProjectionReconciliationIsSourceScopedAndReplaceBased(t *testing.T) {
	raw, err := os.ReadFile("mod_export_content_sync.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, required := range []string{
		"archiveStaleImportedResourceDetailsTx", "incoming_scopes", "incoming_resources",
		"projection_source='import'", "import_source_namespace", "import_source_kind", "import_revision_id",
		"definition=excluded.definition", "localization.provenance='import'",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("import projection reconciliation is missing %q", required)
		}
	}
	if strings.Contains(source, "mod_resource_version_details.definition||excluded.definition") {
		t.Fatal("import projection still merges removed definition keys back into the current fact")
	}
	manualRaw, err := os.ReadFile("mod_content_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	manualResourceRaw, err := os.ReadFile("mod_content_resource_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	manualSource := string(manualRaw) + string(manualResourceRaw)
	if strings.Count(manualSource, "projection_source='manual'") < 2 || strings.Count(manualSource, "import_source_namespace=''") < 2 {
		t.Fatal("manual reserve/publish paths do not take ownership away from the importer")
	}
}

func TestImportedSectionAndPlacementShareClassificationAuthority(t *testing.T) {
	raw, err := os.ReadFile("mod_export_content_sync.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	if strings.Count(source, "when {{resource}}.kind_code in ('minecraft.item','minecraft.block')") != 1 {
		t.Fatal("resource kind classification is duplicated outside its authority expression")
	}
	if strings.Count(source, "like 'gameplay/fishing%'") != 1 {
		t.Fatal("loot category classification is duplicated outside its authority expression")
	}
	if strings.Count(source, "{{template_code}}") != 4 || strings.Count(source, "{{loot_category}}") != 4 {
		t.Fatalf("classification expressions are not shared by section and placement SQL: template=%d loot=%d",
			strings.Count(source, "{{template_code}}"), strings.Count(source, "{{loot_category}}"))
	}
}
