package database

import (
	"strings"
	"testing"
)

func TestCommentPaginationCounterAndKeysetIndexes(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168", schemaGeneration)
	}
	definition := strings.ToLower(strings.Join(commentSchemaStatements(), "\n"))
	for _, required := range []string{
		"create table comment_target_counts",
		"create table comment_target_author_counts",
		"maintain_comment_target_counts",
		"trg_comments_target_counts",
		"idx_comments_target_roots_latest",
		"idx_comments_target_roots_oldest",
		"idx_comments_target_roots_hot_keyset",
		"idx_comments_target_roots_replies_keyset",
		"idx_comments_parent_created_visible",
		"idx_comment_watches_user_created",
		"idx_comment_watches_user_unread",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("comment pagination schema is missing %q", required)
		}
	}
}
