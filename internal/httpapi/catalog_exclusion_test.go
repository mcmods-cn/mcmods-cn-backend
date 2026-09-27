package httpapi

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestCatalogExclusionParsersRejectAmbiguousIdentifiers(t *testing.T) {
	values := url.Values{"excludeSiteId": {" Example_Mod "}}
	if got, err := parseCatalogSlugExclusion(values); err != nil || got != "example_mod" {
		t.Fatalf("slug exclusion=%q err=%v", got, err)
	}
	values.Set("excludeSiteId", "bad/value")
	if _, err := parseCatalogSlugExclusion(values); err == nil {
		t.Fatal("invalid slug exclusion was accepted")
	}
	values.Set("excludeSiteId", "abc123def")
	if got, err := parseCatalogPublicIDExclusion(values); err != nil || got != "abc123def" {
		t.Fatalf("public ID exclusion=%q err=%v", got, err)
	}
	values.Set("excludeSiteId", "project-slug")
	if got, err := parseCatalogPublicIDExclusion(values); err != nil || got != "project-slug" {
		t.Fatalf("cross-catalog slug exclusion=%q err=%v", got, err)
	}
	values.Set("excludeSiteId", "bad/value")
	if _, err := parseCatalogPublicIDExclusion(values); err == nil {
		t.Fatal("invalid cross-catalog exclusion was accepted")
	}
}

func TestEveryProjectCatalogAppliesExclusionInsideItsAuthoritativeCandidateQuery(t *testing.T) {
	expectations := map[string][]string{
		"mod_handlers.go":              {"excludeSiteID == \"\"", "and ($16='' or m.slug<>$16)", "claims.Subject, query, false"},
		"modpack_handlers.go":          {"excludeSiteID == \"\"", "and ($17='' or pack.slug<>$17)", "modFilters, excludeSiteID"},
		"simple_project_handlers.go":   {"excludeSiteID == \"\"", "and ($19='' or project.slug<>$19)", "versionMode, excludeSiteID"},
		"server_catalog_pagination.go": {"ExcludeSiteID", "public_id:!=", "typesenseFilterValue(request.ExcludeSiteID)"},
	}
	for file, fragments := range expectations {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		for _, fragment := range fragments {
			if !strings.Contains(source, fragment) {
				t.Errorf("%s does not bind exclusion fragment %q", file, fragment)
			}
		}
	}
}
