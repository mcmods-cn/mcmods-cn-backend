package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestPlayerProfileListsBatchTextureAssembly(t *testing.T) {
	t.Parallel()
	sourceBytes, err := os.ReadFile("skin_profile_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	start := strings.Index(source, "func (s *Server) loadPlayerProfiles")
	if start < 0 {
		t.Fatal("player profile list loader start missing")
	}
	end := strings.Index(source[start:], "func (s *Server) loadPlayerProfileByPublicIDForViewer")
	if end < 0 {
		t.Fatal("player profile list loader bounds missing")
	}
	body := source[start : start+end]
	if count := strings.Count(body, "loadPlayerTexturesForProfiles("); count != 1 {
		t.Fatalf("player profile list batch texture calls=%d want 1", count)
	}
	if strings.Contains(body, "loadPlayerTextures(ctx, &profiles[index]") {
		t.Fatal("player profile list still loads textures once per profile")
	}
}
