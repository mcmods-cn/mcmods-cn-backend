package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBlueprintPreviewCacheControl(t *testing.T) {
	t.Run("approved is public", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		setBlueprintPreviewCacheControl(recorder, "approved", "public, max-age=60")
		if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=60" {
			t.Fatalf("unexpected cache policy %q", got)
		}
		if got := recorder.Header().Values("Vary"); len(got) != 0 {
			t.Fatalf("approved response unexpectedly varies by credentials: %v", got)
		}
	})
	t.Run("unapproved is credential private", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		setBlueprintPreviewCacheControl(recorder, "pending", "public, max-age=60")
		if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Fatalf("unexpected cache policy %q", got)
		}
		vary := strings.Join(recorder.Header().Values("Vary"), ",")
		if !strings.Contains(vary, "Authorization") || !strings.Contains(vary, "Cookie") {
			t.Fatalf("private preview is missing credential vary headers: %q", vary)
		}
	})
}
