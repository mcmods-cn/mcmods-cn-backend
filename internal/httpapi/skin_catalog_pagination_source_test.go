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
	_, tail, foundStart := strings.Cut(source, "func (s *Server) listPublicSkins")
	body, _, foundEnd := strings.Cut(tail, "func (s *Server) createSkin")
	if !foundStart || !foundEnd {
		t.Fatal("public skin handler bounds missing")
	}
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
