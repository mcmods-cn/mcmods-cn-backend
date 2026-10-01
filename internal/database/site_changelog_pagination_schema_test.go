package database

import (
	"strings"
	"testing"
)

func TestSiteChangelogPaginationUsesGeneration134Indexes(t *testing.T) {
	t.Parallel()
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(governanceAutomationSchemaStatements(), "\n"))
	for _, required := range []string{
		"idx_site_changelogs_public",
		"on site_changelogs(change_date desc,id desc) where status='published'",
		"idx_site_changelogs_admin",
		"on site_changelogs(change_date desc,id desc)",
		"idx_site_changelog_translations_published",
		"site_changelog_translations(changelog_id,locale)",
		"where status='published'",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("site changelog page schema is missing %q", required)
		}
	}
}
