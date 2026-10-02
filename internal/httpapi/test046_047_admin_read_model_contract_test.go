package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestTEST046AndTEST047AdminReadModelsKeepExactPermissionWrappers(t *testing.T) {
	source, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	server := string(source)
	for _, route := range []string{
		`GET /api/v1/admin/site-affairs/about/{locale}", s.requirePermission("site_affairs.about.manage", s.adminAboutPage)`,
		`PUT /api/v1/admin/site-affairs/about/{locale}", s.requirePermission("site_affairs.about.manage", s.adminAboutPage)`,
		`GET /api/v1/admin/site-affairs/changelogs", s.requirePermission("site_affairs.changelog.manage", s.adminSiteChangelogs)`,
		`POST /api/v1/admin/site-affairs/changelogs", s.requirePermission("site_affairs.changelog.manage", s.adminSiteChangelogs)`,
		`GET /api/v1/admin/site-affairs/changelogs/{id}", s.requirePermission("site_affairs.changelog.manage", s.adminSiteChangelogDetail)`,
		`PUT /api/v1/admin/site-affairs/changelogs/{id}", s.requirePermission("site_affairs.changelog.manage", s.adminSiteChangelogDetail)`,
		`GET /api/v1/admin/unresolved-references", s.requirePermission("reference.unresolved.read", s.adminUnresolvedReferences)`,
		`GET /api/v1/admin/unresolved-reference-types", s.requirePermission("reference.unresolved.read", s.adminUnresolvedReferenceTypes)`,
	} {
		if !strings.Contains(server, route) {
			t.Errorf("admin route lost its exact permission wrapper: %s", route)
		}
	}
}
