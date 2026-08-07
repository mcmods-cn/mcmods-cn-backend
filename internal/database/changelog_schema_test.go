package database

import (
	"strings"
	"testing"
)

func TestChangelogSchemaUsesNumericTargetRelationsAndReviewedHistory(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(strings.Join(changelogSchemaStatements(), "\n"))
	for _, expected := range []string{
		"object_route_id bigint not null references public_routes(id)",
		"published_revision_id bigint references content_revisions(id)",
		"source_revision_id bigint not null references content_revisions(id)",
		"created_by bigint not null references users(id)",
		"'project_changelog'",
	} {
		if !strings.Contains(definition, expected) {
			t.Fatalf("changelog schema is missing %q", expected)
		}
	}
	if strings.Contains(definition, "target_public_id") || strings.Contains(definition, "created_by_public_id") {
		t.Fatal("changelog schema stores an external ID as an internal relation")
	}
}
