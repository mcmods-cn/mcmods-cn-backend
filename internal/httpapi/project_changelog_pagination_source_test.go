package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestProjectChangelogListUsesBoundedSummaryPages(t *testing.T) {
	handlerRaw, err := os.ReadFile("project_changelog_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	paginationRaw, err := os.ReadFile("project_changelog_pagination.go")
	if err != nil {
		t.Fatal(err)
	}
	handler := string(handlerRaw)
	source := handler + string(paginationRaw)
	listStart := strings.Index(handler, "func (s *Server) loadProjectChangelogs")
	detailStart := strings.Index(handler, "func (s *Server) loadProjectChangelogEditor")
	if listStart < 0 || detailStart <= listStart {
		t.Fatal("could not isolate project changelog list projection")
	}
	list := handler[listStart:detailStart]
	for _, required := range []string{
		"projectChangelogSummary",
		"parseProjectChangelogPageRequest",
		"projectChangelogPreviewCharacterLimit",
		`"hasMore"`,
		`"nextCursor"`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("project changelog pagination is missing %q", required)
		}
	}
	if strings.Contains(list, "jsonb_object_agg(value.locale,value.body_markdown)") {
		t.Fatal("project changelog list still aggregates every localized body")
	}
	if !strings.Contains(list, "limit") {
		t.Fatal("project changelog list does not contain a hard SQL limit")
	}
}
