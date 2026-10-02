package database

import (
	"strings"
	"testing"
)

func TestBUG034ManualChangelogOverrideHasApprovedRevisionProvenance(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168 for approved manual-override provenance", schemaGeneration)
	}
	schema := strings.Join(governanceAutomationSchemaStatements(), "\n")
	for _, required := range []string{
		"manual_override_revision_id bigint references content_revisions(id) on delete restrict",
		"manual_override_source text not null default ''",
		"external_release_bindings_manual_override_origin_check",
		"idx_external_release_bindings_manual_override_revision",
	} {
		if !strings.Contains(schema, required) {
			t.Errorf("external release binding schema is missing %q", required)
		}
	}
}
