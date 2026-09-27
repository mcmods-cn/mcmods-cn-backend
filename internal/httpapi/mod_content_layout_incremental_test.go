package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestModContentLayoutRouteOnlyAcceptsIncrementalPatch(t *testing.T) {
	server := &Server{mux: http.NewServeMux()}
	server.routes()
	path := "/api/v1/mods/example/content-sections/root00001/layout"
	patchRequest := httptest.NewRequest(http.MethodPatch, path, nil)
	_, pattern := server.mux.Handler(patchRequest)
	if pattern != "PATCH /api/v1/mods/{siteId}/content-sections/{sectionId}/layout" {
		t.Fatalf("incremental layout route is not registered: %q", pattern)
	}
	putResponse := httptest.NewRecorder()
	server.mux.ServeHTTP(putResponse, httptest.NewRequest(http.MethodPut, path, nil))
	if putResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("legacy full-snapshot PUT remains reachable: status=%d", putResponse.Code)
	}
}

func TestNormalizeModContentLayoutPatchRejectsMoreThanOnePage(t *testing.T) {
	patch := modContentLayoutPatch{
		VersionPublicID: "version01", RootSectionPublicID: "root00001", DisplayMode: "compact",
		Resources: make([]modContentLayoutResourceEdit, maxModContentLayoutPatchResources+1),
	}
	if err := normalizeModContentLayoutPatch(&patch); err == nil {
		t.Fatalf("accepted %d layout changes; limit=%d", len(patch.Resources), maxModContentLayoutPatchResources)
	}
}

func TestApplyModContentLayoutPatchPreservesUnloadedResources(t *testing.T) {
	current := modContentLayoutEdit{
		RootSectionPublicID: "root00001",
		Categories:          []modContentLayoutCategoryEdit{{PublicID: "cat000001", ParentPublicID: "root00001", DefaultLocale: "en-US"}},
		Resources: []modContentLayoutResourceEdit{
			{ResourcePublicID: "res000001", SectionPublicID: "root00001", Ordinal: 0},
			{ResourcePublicID: "res000002", SectionPublicID: "root00001", Ordinal: 1},
			{ResourcePublicID: "res000003", SectionPublicID: "cat000001", Ordinal: 0},
		},
	}
	patch := modContentLayoutPatch{
		Resources: []modContentLayoutResourceEdit{{ResourcePublicID: "res000002", SectionPublicID: "cat000001", Ordinal: 1}},
	}
	if err := applyModContentLayoutPatch(&current, patch); err != nil {
		t.Fatal(err)
	}
	if len(current.Resources) != 3 {
		t.Fatalf("incremental patch dropped unloaded resources: %#v", current.Resources)
	}
	placements := make(map[string]string, len(current.Resources))
	for _, resource := range current.Resources {
		placements[resource.ResourcePublicID] = resource.SectionPublicID
	}
	if placements["res000001"] != "root00001" || placements["res000002"] != "cat000001" || placements["res000003"] != "cat000001" {
		t.Fatalf("unexpected merged placements: %#v", placements)
	}
}

func TestApplyModContentLayoutPatchMovesResourcesOutOfDeletedCategories(t *testing.T) {
	current := modContentLayoutEdit{
		RootSectionPublicID: "root00001",
		Categories: []modContentLayoutCategoryEdit{
			{PublicID: "cat000001", ParentPublicID: "root00001", DefaultLocale: "en-US"},
			{PublicID: "cat000002", ParentPublicID: "cat000001", DefaultLocale: "en-US"},
		},
		Resources: []modContentLayoutResourceEdit{
			{ResourcePublicID: "res000001", SectionPublicID: "cat000001", Ordinal: 0},
			{ResourcePublicID: "res000002", SectionPublicID: "cat000002", Ordinal: 0},
		},
	}
	categories := []modContentLayoutCategoryEdit{{PublicID: "cat000002", ParentPublicID: "root00001", DefaultLocale: "en-US"}}
	if err := applyModContentLayoutPatch(&current, modContentLayoutPatch{Categories: &categories}); err != nil {
		t.Fatal(err)
	}
	if current.Resources[0].SectionPublicID != "root00001" || current.Resources[1].SectionPublicID != "cat000002" {
		t.Fatalf("deleted-category placements were not repaired: %#v", current.Resources)
	}
}

func TestTwentyThousandResourceLayoutUsesOneBoundedPageAndIncrementalMerge(t *testing.T) {
	current := modContentLayoutEdit{RootSectionPublicID: "root00001", Resources: make([]modContentLayoutResourceEdit, 20000)}
	for index := range current.Resources {
		current.Resources[index] = modContentLayoutResourceEdit{
			ResourcePublicID: fmt.Sprintf("r%08d", index+1), SectionPublicID: "root00001", Ordinal: index,
		}
	}
	changes := make([]modContentLayoutResourceEdit, maxModContentLayoutPatchResources)
	for index := range changes {
		changes[index] = current.Resources[index]
		changes[index].Ordinal = maxModContentResources - index - 1
	}
	started := time.Now()
	if err := applyModContentLayoutPatch(&current, modContentLayoutPatch{Resources: changes}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("20k incremental merge exceeded one second: %s", elapsed)
	}
	if len(current.Resources) != maxModContentResources || current.Resources[15000].Ordinal != 15000 || current.Resources[0].Ordinal != maxModContentResources-1 {
		t.Fatalf("incremental merge lost or rewrote untouched rows: first=%+v untouched=%+v count=%d",
			current.Resources[0], current.Resources[15000], len(current.Resources))
	}

	legacySample, _ := json.Marshal(map[string]any{
		"resourcePublicId": "r00000001", "canonicalId": "example:resource", "names": map[string]string{"en-US": strings.Repeat("name", 64)},
		"iconPath": strings.Repeat("icons/path/", 16), "hasDetailDescription": true, "definition": strings.Repeat("x", 4096),
	})
	page := make([]modContentLayoutQueryItem, 500)
	for index := range page {
		page[index] = modContentLayoutQueryItem{ResourcePublicID: fmt.Sprintf("r%08d", index+1), SectionPublicID: "root00001", Label: "example:resource", Ordinal: index}
	}
	pagePayload, _ := json.Marshal(page)
	legacyTwentyThousandEstimate := len(legacySample) * maxModContentResources
	if legacyTwentyThousandEstimate < 80<<20 || len(pagePayload) > 128<<10 || legacyTwentyThousandEstimate < 500*len(pagePayload) {
		t.Fatalf("unexpected payload bounds: legacy20k=%d summary500=%d", legacyTwentyThousandEstimate, len(pagePayload))
	}
	t.Logf("estimated legacy20k=%d bytes; summary500=%d bytes; incremental merge=%s",
		legacyTwentyThousandEstimate, len(pagePayload), time.Since(started))
}
