package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCatalogHandlersUseBoundedCardProjections(t *testing.T) {
	tests := []struct {
		file      string
		start     string
		end       string
		required  []string
		forbidden []string
	}{
		{
			file:  "simple_project_handlers.go",
			start: "func (s *Server) simpleProjects(",
			end:   "func (s *Server) simpleProjectItem(",
			required: []string{
				"scanSimpleProjectCatalogCard",
				"loadSimpleProjectCatalogAssociations",
				"writeBoundedCatalogJSON",
			},
			forbidden: []string{"loadSimpleProjectAssociations"},
		},
		{
			file:  "modpack_handlers.go",
			start: "func (s *Server) modpacks(",
			end:   "func (s *Server) modpackItem(",
			required: []string{
				"scanModpackCatalogCard",
				"loadModpackCatalogAssociations",
				"writeBoundedCatalogJSON",
			},
			forbidden: []string{"pack.body_markdown", "loadModpackAssociations"},
		},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			raw, err := os.ReadFile(test.file)
			if err != nil {
				t.Fatal(err)
			}
			source := string(raw)
			start := strings.Index(source, test.start)
			end := strings.Index(source, test.end)
			if start < 0 || end <= start {
				t.Fatalf("could not isolate catalog handler")
			}
			handler := source[start:end]
			for _, value := range test.required {
				if !strings.Contains(handler, value) {
					t.Errorf("catalog handler must contain %q", value)
				}
			}
			for _, value := range test.forbidden {
				if strings.Contains(handler, value) {
					t.Errorf("catalog handler must not contain detail-only operation %q", value)
				}
			}
		})
	}
}

func TestCatalogResponsesHaveAnAtomicByteBudget(t *testing.T) {
	raw, err := os.ReadFile("response.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, required := range []string{
		"maxCatalogResponseBytes = 2 << 20",
		"func writeBoundedCatalogJSON(",
		"json.Marshal(apiResponse{Data: data})",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("bounded catalog response contract must contain %q", required)
		}
	}
}

func TestBoundedCatalogResponseRejectsBeforeWritingAnOversizedPage(t *testing.T) {
	oversized := httptest.NewRecorder()
	if writeBoundedCatalogJSON(oversized, map[string]string{"value": strings.Repeat("x", maxCatalogResponseBytes)}) {
		t.Fatal("oversized catalog response was accepted")
	}
	if oversized.Code != http.StatusInternalServerError || oversized.Body.Len() >= maxCatalogResponseBytes {
		t.Fatalf("oversized response was not replaced atomically: status=%d bytes=%d", oversized.Code, oversized.Body.Len())
	}

	bounded := httptest.NewRecorder()
	if !writeBoundedCatalogJSON(bounded, map[string]string{"value": strings.Repeat("x", maxCatalogResponseBytes-128)}) {
		t.Fatal("response below the catalog byte budget was rejected")
	}
	if bounded.Code != http.StatusOK || bounded.Body.Len() > maxCatalogResponseBytes {
		t.Fatalf("bounded response contract failed: status=%d bytes=%d", bounded.Code, bounded.Body.Len())
	}
}
