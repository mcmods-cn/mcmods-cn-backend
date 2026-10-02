package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestCommunityPostRevisionBaselineIsLockedBeforeSideEffectsSource(t *testing.T) {
	raw, err := os.ReadFile("community_post_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func (s *Server) updateCommunityPost(")
	end := strings.Index(source, "func lockCommunityPostRevisionBaseTx(")
	if start < 0 || end <= start {
		t.Fatal("community post update source bounds were not found")
	}
	handler := source[start:end]
	lockAt := strings.Index(handler, "lockCommunityPostRevisionBaseTx(")
	resolveAt := strings.Index(handler, "s.resolveCommunityPostSnapshot(")
	bountyAt := strings.Index(handler, "validateCommunityPostBountyEditTx(")
	revisionAt := strings.Index(handler, "createContentRevisionTx(")
	if lockAt < 0 || resolveAt < 0 || bountyAt < 0 || revisionAt < 0 || lockAt > resolveAt || lockAt > bountyAt || lockAt > revisionAt {
		t.Fatal("the published revision baseline must be locked and compared before every update side effect")
	}
	helperEnd := strings.Index(source[end+1:], "\nfunc ")
	if helperEnd < 0 {
		t.Fatal("community post revision lock helper end was not found")
	}
	helper := source[end : end+1+helperEnd]
	advisoryAt := strings.Index(helper, "pg_advisory_xact_lock")
	currentAt := strings.Index(helper, "post.published_revision_id")
	if advisoryAt < 0 || currentAt < 0 || advisoryAt > currentAt || !strings.Contains(helper, "currentPublicID != expectedPublicID") {
		t.Fatal("the aggregate lock must precede the authoritative published revision comparison")
	}
	if !strings.Contains(source, "coalesce(revision.public_id,'')") || !strings.Contains(source, "PublishedRevisionID") {
		t.Fatal("community post detail must expose the public revision baseline")
	}
}
