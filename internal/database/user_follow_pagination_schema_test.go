package database

import (
	"strings"
	"testing"
)

func TestUserFollowCursorIndexesBelongToGeneration106(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(baselineSchemaStatements(), "\n"))
	for _, index := range []string{
		"idx_user_follows_followed_page on user_follows (followed_id, created_at desc, follower_id desc)",
		"idx_user_follows_follower_page on user_follows (follower_id, created_at desc, followed_id desc)",
	} {
		if !strings.Contains(schema, index) {
			t.Fatalf("user follow schema is missing %q", index)
		}
	}
}
