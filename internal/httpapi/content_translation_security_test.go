package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestCatalogContentReadCannotEnqueueAIWork(t *testing.T) {
	source, err := os.ReadFile("content_localization_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, "func (s *Server) catalogEntityContent(")
	end := strings.Index(text, "func (s *Server) updateCatalogEntityContent(")
	if start < 0 || end <= start {
		t.Fatal("catalog content handler boundaries were not found")
	}
	readHandler := text[start:end]
	for _, forbidden := range []string{"enqueueCatalogContentTranslation", "publishContentTranslationTask", "maxPermissionValue"} {
		if strings.Contains(readHandler, forbidden) {
			t.Fatalf("public GET still contains AI side effect %q", forbidden)
		}
	}
	if !strings.Contains(text, "actorID <= 0") || !strings.Contains(text, "reserveAITaskQuotaTx") {
		t.Fatal("translation enqueue boundary must reject anonymous actors and reserve quota")
	}
}
