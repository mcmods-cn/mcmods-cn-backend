package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestModContentSectionRouteUsesLayoutAsOnlyUpdateAuthority(t *testing.T) {
	server := &Server{mux: http.NewServeMux()}
	server.routes()
	path := "/api/v1/mods/example/content-sections/section01"

	deleteRequest := httptest.NewRequest(http.MethodDelete, path, nil)
	_, pattern := server.mux.Handler(deleteRequest)
	if pattern != "DELETE /api/v1/mods/{siteId}/content-sections/{sectionId}" {
		t.Fatalf("section archive route is not registered: %q", pattern)
	}
	putResponse := httptest.NewRecorder()
	server.mux.ServeHTTP(putResponse, httptest.NewRequest(http.MethodPut, path, nil))
	if putResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("legacy single-section PUT remains reachable: status=%d", putResponse.Code)
	}

	serverSource, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	handlerSource, err := os.ReadFile("mod_content_resource_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serverSource), `PUT /api/v1/mods/{siteId}/content-sections/{sectionId}`) {
		t.Fatal("legacy single-section PUT route is still registered")
	}
	if strings.Contains(string(handlerSource), `Kind: "section", Operation: "edit"`) {
		t.Fatal("legacy single-section edit submission branch still exists")
	}
}

func TestNewModContentSectionMustBeAnEmptyRoot(t *testing.T) {
	valid := func() modContentSectionEdit {
		return modContentSectionEdit{
			VersionPublicID: "version01", TemplatePublicID: "template01", DefaultLocale: "en-US",
			DisplayMode: "compact", Reason: "create root",
		}
	}
	withParent := valid()
	withParent.ParentPublicID = "parent001"
	if err := normalizeModContentSectionEdit(&withParent); err == nil {
		t.Fatal("new section accepted a parent outside the authoritative layout mutation")
	}
	withResource := valid()
	withResource.Resources = []modContentSectionResourceEdit{{
		VersionPublicID: "version01", ResourcePublicID: "resource01", Ordinal: 0,
	}}
	if err := normalizeModContentSectionEdit(&withResource); err == nil {
		t.Fatal("new section accepted resource placements outside the authoritative layout mutation")
	}
	healthy := valid()
	if err := normalizeModContentSectionEdit(&healthy); err != nil {
		t.Fatalf("empty root section was rejected: %v", err)
	}
}
