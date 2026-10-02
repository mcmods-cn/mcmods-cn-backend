package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestModRelationshipGroupsLoadInOneOrderedQuery(t *testing.T) {
	source, err := os.ReadFile("mod_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	content := string(source)
	start := strings.Index(content, "func (s *Server) loadModAssociations")
	end := strings.Index(content, "func (s *Server) loadIncomingModRelationships")
	if start < 0 || end <= start {
		t.Fatal("Mod association loader inventory changed")
	}
	loader := content[start:end]
	if strings.Contains(loader, "for _, group := range groups") {
		t.Fatal("Mod relationship loader still queries once per group")
	}
	if strings.Count(loader, "from mod_relationship_groups") != 1 || !strings.Contains(loader, "left join lateral") {
		t.Fatal("Mod relationship groups and visible relationships do not share one joined query")
	}
}
