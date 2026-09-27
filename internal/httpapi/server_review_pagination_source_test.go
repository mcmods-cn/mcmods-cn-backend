package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestServerReviewQueueExposesIndexedCursorPagination(t *testing.T) {
	handler, err := os.ReadFile("server_review_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	pagination, err := os.ReadFile("server_review_pagination.go")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile("../database/server_schema.go")
	if err != nil {
		t.Fatal(err)
	}
	combined := string(handler) + string(pagination) + string(schema)
	for _, required := range []string{
		`"hasMore"`, `"nextCursor"`, `request.Cursor.CreatedAt`,
		`idx_minecraft_servers_review_page`, `review_status,created_at,id`,
	} {
		if !strings.Contains(combined, required) {
			t.Errorf("server review pagination is missing %q", required)
		}
	}
	if strings.Contains(string(handler), "limit 200") {
		t.Fatal("fixed server review queue truncation remains")
	}
}
