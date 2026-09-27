package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestLegacyMultipartOptInAndCommentReportRouteAreRemoved(t *testing.T) {
	t.Parallel()
	files := map[string][]string{
		"oss_handlers.go":     {"PreferMultipart", "preferMultipart", "req.PreferMultipart"},
		"oss_multipart.go":    {"preferred bool"},
		"comment_handlers.go": {"type reportCommentRequest", "func (s *Server) reportComment"},
		"server.go":           {`POST /api/v1/comments/{commentId}/reports`, "s.reportComment"},
	}
	for file, forbidden := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range forbidden {
			if strings.Contains(string(raw), value) {
				t.Fatalf("%s still contains legacy contract %q", file, value)
			}
		}
	}
	if !shouldUseOSSMultipart(ossMultipartThreshold) || shouldUseOSSMultipart(ossMultipartThreshold-1) {
		t.Fatal("multipart selection is not controlled exclusively by the server threshold")
	}
}
