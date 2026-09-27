package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestPublicSkinCatalogUsesBoundedProjectionCursorPage(t *testing.T) {
	t.Parallel()
	sourceBytes, err := os.ReadFile("skin_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	start := strings.Index(source, "func (s *Server) listPublicSkins")
	end := strings.Index(source[start:], "func (s *Server) createSkin")
	if start < 0 || end < 0 {
		t.Fatal("public skin handler bounds missing")
	}
	body := source[start : start+end]
	for _, required := range []string{"parseSkinCatalogPageRequest", "skinCatalogPageSQL", "hasMore", "nextCursor"} {
		if !strings.Contains(body, required) {
			t.Fatalf("public skin handler is missing %q", required)
		}
	}
	for _, forbidden := range []string{"boundedOffset", " offset $", "count(*)", "array_to_string", " ilike "} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("public skin handler still contains %q", forbidden)
		}
	}
}
