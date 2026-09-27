package httpapi

import (
	"regexp"
	"strings"
	"testing"
)

func TestUserShowcaseProjectsQueryStartsFromUserAccess(t *testing.T) {
	query := strings.ToLower(regexp.MustCompile(`\s+`).ReplaceAllString(userShowcaseProjectsQuery, " "))
	for _, required := range []string{
		"qualified_access as materialized",
		"from effective_project_access access where access.user_id=$1",
		"group by access.project_type,access.project_id",
		"from qualified_access access join mods mod",
		"from qualified_access access join modpacks pack",
		"from qualified_access access join simple_projects project",
		"from qualified_access access join minecraft_servers server",
		"from qualified_access access join blueprints blueprint",
		"from qualified_access access join skin_assets asset",
		"from qualified_access access join community_posts post",
	} {
		if !strings.Contains(query, required) {
			t.Fatalf("showcase query is missing access-first fragment %q", required)
		}
	}
	if strings.Contains(query, "exists(select 1 from effective_project_access") {
		t.Fatal("showcase query still probes access after building the site-wide project union")
	}
}
