package httpapi

import (
	"strings"
	"testing"

	"mcmods-cn-backend/internal/serverprobe"
)

func TestNormalizeAndValidateServerRequest(t *testing.T) {
	request := createMinecraftServerRequest{
		Address:           " play.example.net ",
		Name:              " Example Server ",
		ShortDescription:  " A server ",
		MinecraftVersions: []string{"1.21.1", "1.21.1"},
		Languages:         []string{"zh-CN", "zh-CN"},
		PrimaryTag:        "RPG",
		ProofText:         "Control panel screenshot",
		ProofFileIDs:      []string{"abc123def"},
	}
	if err := normalizeAndValidateServerRequest(&request, defaultServerCatalogSettings()); err != nil {
		t.Fatal(err)
	}
	if request.Name != "Example Server" || request.PrimaryTag != "rpg" ||
		len(request.MinecraftVersions) != 1 || len(request.Languages) != 1 {
		t.Fatalf("request was not normalized: %#v", request)
	}
}

func TestNormalizeAndValidateServerRequestRejectsMarkdownSizedSummary(t *testing.T) {
	request := createMinecraftServerRequest{
		Address:           "play.example.net",
		Name:              "Example",
		ShortDescription:  strings.Repeat("界", 241),
		MinecraftVersions: []string{"1.21.1"},
		Languages:         []string{"zh-CN"},
		PrimaryTag:        "survival",
		ProofText:         "proof",
		ProofFileIDs:      []string{"abc123def"},
	}
	if err := normalizeAndValidateServerRequest(&request, defaultServerCatalogSettings()); err == nil {
		t.Fatal("oversized summary was accepted")
	}
}

func TestMergeServerModRequestsPrefersDetectedEvidence(t *testing.T) {
	result := mergeServerModRequests(
		[]serverprobe.Mod{{ID: "Example_Mod", Version: "1.0", Source: "forge_status", Confidence: "exact"}},
		[]createServerModRequest{
			{ID: "example_mod", Source: "manual", Confidence: "declared"},
			{ID: "missing_mod", Source: "manual", Confidence: "declared"},
		},
	)
	if len(result) != 2 {
		t.Fatalf("got %d mods, want 2", len(result))
	}
	if result[0].ID != "example_mod" || result[0].Source != "forge_status" || result[0].Confidence != "exact" {
		t.Fatalf("detected evidence was overwritten: %#v", result[0])
	}
}

func TestNormalizeAndValidateServerUpdateRequestReusesMetadataRules(t *testing.T) {
	request := updateMinecraftServerRequest{
		Name:              " Updated Server ",
		ShortDescription:  " Updated summary ",
		MinecraftVersions: []string{"1.21.1", "1.21.1"},
		Languages:         []string{"zh-CN", "zh-CN"},
		PrimaryTag:        "Technology",
		Mods: []createServerModRequest{
			{ID: "example_mod", Source: "manual", Confidence: "declared"},
		},
	}
	if err := normalizeAndValidateServerUpdateRequest(&request, defaultServerCatalogSettings()); err != nil {
		t.Fatal(err)
	}
	if request.Name != "Updated Server" || request.PrimaryTag != "technology" ||
		len(request.MinecraftVersions) != 1 || len(request.Languages) != 1 {
		t.Fatalf("update request was not normalized: %#v", request)
	}
}

func TestServerModFiltersNormalizesAndDeduplicates(t *testing.T) {
	filters := serverModFilters(" Example_Mod,example_mod,invalid:mod, second.mod ")
	if len(filters) != 2 || filters[0] != "example_mod" || filters[1] != "second.mod" {
		t.Fatalf("unexpected filters: %#v", filters)
	}
}
