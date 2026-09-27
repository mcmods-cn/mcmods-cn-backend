package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestMyCommentWatchesBatchAssemblesCommentsAndTargets(t *testing.T) {
	t.Parallel()
	sourceBytes, err := os.ReadFile("comment_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	detailBytes, err := os.ReadFile("comment_detail_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes) + "\n" + string(detailBytes)
	start := strings.Index(source, "func (s *Server) myCommentWatches")
	end := strings.Index(source[start:], "func (s *Server) commentWatchItem")
	if start < 0 || end < 0 {
		t.Fatal("watch handler bounds missing")
	}
	body := source[start : start+end]
	if count := strings.Count(body, "queryCommentItems("); count != 1 {
		t.Fatalf("watch page comment assembly calls=%d want 1", count)
	}
	if !strings.Contains(body, "queryCommentTargetsByInternal") {
		t.Fatal("watch page is missing batched target resolution")
	}
	if strings.Contains(body, "resolveCommentTargetByInternal") {
		t.Fatal("watch page still resolves targets per item")
	}
	if !strings.Contains(body, "commentsByID") || !strings.Contains(body, "targetsByIdentity") {
		t.Fatal("watch page does not assemble from batch result maps")
	}
}

func TestCommentAuthorAvatarsAreResolvedOncePerUniqueStoredURL(t *testing.T) {
	t.Parallel()
	sourceBytes, err := os.ReadFile("comment_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	detailBytes, err := os.ReadFile("comment_detail_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes) + "\n" + string(detailBytes)
	start := strings.Index(source, "func (s *Server) queryCommentItems")
	end := strings.Index(source[start:], "var errCommentAttachmentUnavailable")
	if start < 0 || end < 0 {
		t.Fatal("comment assembly bounds missing")
	}
	body := source[start : start+end]
	if !strings.Contains(body, "resolvedAvatarURLs") {
		t.Fatal("comment assembly does not cache unique avatar access URLs")
	}
}
