package database

import (
	"strings"
	"testing"
)

func TestUserBlockSchemaDefinesDirectionalIndexedRelationship(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(strings.Join(userBlockSchemaStatements(), "\n"))
	for _, required := range []string{
		"create table if not exists user_blocks",
		"primary key(blocker_id,blocked_id)",
		"check(blocker_id<>blocked_id)",
		"idx_user_blocks_blocked_blocker",
		"on user_blocks(blocked_id,blocker_id)",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("user block schema is missing %q", required)
		}
	}
}

func TestCommentPopularityRouteFunctionUsesUnambiguousParameters(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(contentRouteForCommentFunctionStatement())
	for _, required := range []string{
		"content_route_for_comment.kind",
		"route.internal_id=content_route_for_comment.internal_id",
		"version.id=content_route_for_comment.version_id",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("comment route function is missing %q", required)
		}
	}
	for _, forbidden := range []string{"route.internal_id=internal_id", "version.id=version_id"} {
		if strings.Contains(definition, forbidden) {
			t.Fatalf("comment route function keeps ambiguous expression %q", forbidden)
		}
	}
}

func TestCommentPopularityRefreshUsesUnambiguousAuthorVariable(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(contentPopularityCommentRefreshFunctionStatement())
	if !strings.Contains(definition, "comment.author_id=changed.author_id") {
		t.Fatal("comment popularity refresh does not use its unambiguous author variable")
	}
	if strings.Contains(definition, "comment.author_id=author_id") {
		t.Fatal("comment popularity refresh keeps its ambiguous author expression")
	}
}
