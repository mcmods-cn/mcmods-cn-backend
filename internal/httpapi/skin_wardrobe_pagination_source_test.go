package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestSkinWardrobeHandlerUsesCursorPageEnvelope(t *testing.T) {
	raw, err := os.ReadFile("skin_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	_, tail, foundStart := strings.Cut(source, "func (s *Server) skinWardrobe(")
	handler, _, foundEnd := strings.Cut(tail, "func (s *Server) skinWardrobeItem(")
	if !foundStart || !foundEnd {
		t.Fatal("skin wardrobe handler boundaries were not found")
	}
	for _, required := range []string{
		"parseSkinWardrobePageRequest", "skinWardrobePageSQL", "skinWardrobeCountSQL",
		`"hasMore"`, `"nextCursor"`,
	} {
		if !strings.Contains(handler, required) {
			t.Errorf("wardrobe handler missing %q", required)
		}
	}
	for _, forbidden := range []string{"maximumSkinWardrobeItems)", `"offset"`} {
		if strings.Contains(handler, forbidden) {
			t.Errorf("wardrobe handler retains fixed-window token %q", forbidden)
		}
	}
}
