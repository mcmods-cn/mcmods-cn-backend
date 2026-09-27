package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestCommentListHandlersUseKeysetPaginationAndCounterFacts(t *testing.T) {
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
	for _, bounds := range [][2]string{
		{"func (s *Server) listTargetComments", "func (s *Server) createComment"},
		{"func (s *Server) commentReplies", "func (s *Server) commentItem"},
		{"func (s *Server) myCommentWatches", "func (s *Server) commentWatchItem"},
	} {
		start := strings.Index(source, bounds[0])
		end := strings.Index(source[start:], bounds[1])
		if start < 0 || end < 0 {
			t.Fatalf("handler bounds missing: %q", bounds)
		}
		body := source[start : start+end]
		if strings.Contains(strings.ToLower(body), " offset ") || strings.Contains(body, "nonNegativeInt") {
			t.Fatalf("OFFSET pagination remains in %s", bounds[0])
		}
	}

	rootStart := strings.Index(source, "func (s *Server) listTargetComments")
	rootEnd := strings.Index(source[rootStart:], "func (s *Server) createComment")
	rootBody := source[rootStart : rootStart+rootEnd]
	if strings.Contains(strings.ToLower(rootBody), "select count(*) from comments") {
		t.Fatal("root comment list still performs a full COUNT")
	}
	for _, required := range []string{"decodeCommentRootPageCursor", "queryCommentTargetVisibleTotal"} {
		if !strings.Contains(rootBody, required) {
			t.Fatalf("root comment list is missing %s", required)
		}
	}
}
