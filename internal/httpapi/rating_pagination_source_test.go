package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestRatingReviewsHandlerUsesCursorEnvelopeWithoutSynchronousCount(t *testing.T) {
	raw, err := os.ReadFile("rating_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func (s *Server) ratingReviews(")
	end := strings.Index(source[start:], "func (s *Server) resolveRateableTarget(")
	if start < 0 || end < 0 {
		t.Fatal("rating review handler boundaries were not found")
	}
	handler := source[start : start+end]
	for _, required := range []string{
		"parseRatingReviewPageRequest", "ratingReviewPageSQL", `"hasMore"`, `"nextCursor"`,
	} {
		if !strings.Contains(handler, required) {
			t.Errorf("rating handler missing %q", required)
		}
	}
	for _, forbidden := range []string{"select count(*) from content_ratings", "boundedOffset", `"offset"`} {
		if strings.Contains(handler, forbidden) {
			t.Errorf("rating handler retains linear pagination token %q", forbidden)
		}
	}
}
