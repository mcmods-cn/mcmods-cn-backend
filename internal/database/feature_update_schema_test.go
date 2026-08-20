package database

import (
	"strings"
	"testing"
)

func TestFeatureUpdateSchemaContainsStableRelationsAndIndexes(t *testing.T) {
	definition := strings.ToLower(strings.Join(featureUpdateSchemaStatements(), "\n"))
	for _, required := range []string{
		"primary key(recipe_id,version_code)",
		"comment_floor_counters",
		"idx_comments_target_floor",
		"public_code text not null unique",
		"comment_log_bindings",
		"idx_log_shares_expiry",
		"idx_log_shares_owner_fk",
		"idx_log_shares_source_file_fk",
		"idx_comment_log_bindings_attachment",
		"comment_attachments",
		"idx_comment_attachments_file",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("feature update schema is missing %q", required)
		}
	}
	if strings.Contains(definition, "max(floor_number) + 1") {
		t.Fatal("comment floor assignment must not use a racy max+1 calculation")
	}
}
