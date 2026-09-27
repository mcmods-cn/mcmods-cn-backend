package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestProjectAuthorshipQueueExposesIndexedCursorPagination(t *testing.T) {
	handler, err := os.ReadFile("project_authorship_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile("../database/migrations.go")
	if err != nil {
		t.Fatal(err)
	}
	combined := string(handler) + string(schema)
	for _, required := range []string{
		`"hasMore"`, `"nextCursor"`, `request.Cursor.CreatedAt`,
		`idx_content_creator_bindings_review_page`, `status,created_at,id`,
	} {
		if !strings.Contains(combined, required) {
			t.Errorf("project authorship pagination is missing %q", required)
		}
	}
	if strings.Contains(string(handler), "limit 200") {
		t.Fatal("fixed project authorship queue truncation remains")
	}
}
