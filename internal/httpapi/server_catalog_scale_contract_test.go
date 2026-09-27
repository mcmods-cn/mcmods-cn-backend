package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestPublicMinecraftServersUsesAuthoritativeIndexCursorWithoutDatabaseDeepPaging(t *testing.T) {
	source, err := os.ReadFile("server_catalog_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(source)
	start := strings.Index(contents, "func (s *Server) publicMinecraftServers(")
	end := strings.Index(contents, "func (s *Server) publicMinecraftServerDetail(")
	if start < 0 || end <= start {
		t.Fatal("could not isolate publicMinecraftServers")
	}
	handler := strings.ToLower(contents[start:end])
	for _, forbidden := range []string{
		"select count(",
		" offset ",
		" ilike ",
		"plainto_tsquery",
		"minecraft_server_mods filter_mod",
	} {
		if strings.Contains(handler, forbidden) {
			t.Fatalf("server catalog retained database deep-pagination/search fragment %q", forbidden)
		}
	}
	for _, required := range []string{
		"parseServerCatalogPageRequest",
		"searchServerPage",
		"hasMore",
		"nextCursor",
		"StatusServiceUnavailable",
	} {
		if !strings.Contains(contents[start:end], required) {
			t.Fatalf("server catalog is missing cursor/index contract %q", required)
		}
	}
}

func TestServerCatalogDatabaseFallbackIsCountlessKeysetAndHasOneIndexedTextPredicate(t *testing.T) {
	source, err := os.ReadFile("server_catalog_database_page.go")
	if err != nil {
		t.Fatal(err)
	}
	contents := strings.ToLower(string(source))
	for _, forbidden := range []string{"select count(", " offset ", " ilike ", " or server.name", " or exists"} {
		if strings.Contains(contents, forbidden) {
			t.Fatalf("server database fallback retained %q", forbidden)
		}
	}
	for _, required := range []string{"plainto_tsquery", "servercatalogdatabasecursorpredicate", "limit+1"} {
		if !strings.Contains(contents, required) {
			t.Fatalf("server database fallback is missing %q", required)
		}
	}
}
