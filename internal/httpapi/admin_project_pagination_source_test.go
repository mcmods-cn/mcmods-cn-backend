package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestAdminProjectListUsesSearchProjectionAndKeysetEnvelope(t *testing.T) {
	source, err := os.ReadFile("admin_dashboard_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, "func (s *Server) adminDashboardProjects")
	if start < 0 {
		t.Fatal("could not find admin project list handler")
	}
	remainder := text[start:]
	end := strings.Index(remainder, "func (s *Server) adminDashboardProject(")
	if end <= 0 {
		t.Fatal("could not isolate admin project list handler")
	}
	// This gate verifies that the HTTP handler forwards pagination and keeps
	// the keyset envelope. The query builder and PERF036 exercise actual
	// filters, bounded search, ordered result IDs and index plans.
	handler := strings.ToLower(remainder[:end])
	for _, required := range []string{
		"parseadminprojectpagerequest",
		"adminprojectpagesql(",
		"hasmore",
		"nextcursor",
	} {
		if !strings.Contains(handler, required) {
			t.Errorf("admin project list handler is missing %q", required)
		}
	}
	for _, forbidden := range []string{"boundedoffset", "select count(", " ilike ", " offset ", `"total"`} {
		if strings.Contains(handler, forbidden) {
			t.Errorf("admin project list retained unbounded path %q", forbidden)
		}
	}
}
