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
	if err := normalizeAndValidateServerRequest(&request, defaultServerCatalogSettings(), true); err != nil {
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
	if err := normalizeAndValidateServerRequest(&request, defaultServerCatalogSettings(), true); err == nil {
		t.Fatal("oversized summary was accepted")
	}
}

func TestNormalizeAndValidateServerRequestAllowsNoProofWhenReviewDisabled(t *testing.T) {
	request := createMinecraftServerRequest{
		Address:           "play.example.net",
		Name:              "Example",
		MinecraftVersions: []string{"1.21.1"},
		Languages:         []string{"zh-CN"},
		PrimaryTag:        "survival",
		ProofText:         "must be discarded",
		ProofFileIDs:      []string{"abc123def"},
	}
	if err := normalizeAndValidateServerRequest(&request, defaultServerCatalogSettings(), false); err != nil {
		t.Fatal(err)
	}
	if request.ProofText != "" || len(request.ProofFileIDs) != 0 {
		t.Fatalf("disabled review retained proof data: %#v", request)
	}
}

func TestNormalizeAndValidateServerRequestRequiresProofWhenReviewEnabled(t *testing.T) {
	request := createMinecraftServerRequest{
		Address:           "play.example.net",
		Name:              "Example",
		MinecraftVersions: []string{"1.21.1"},
		Languages:         []string{"zh-CN"},
		PrimaryTag:        "survival",
	}
	if err := normalizeAndValidateServerRequest(&request, defaultServerCatalogSettings(), true); err == nil {
		t.Fatal("review-enabled server request without proof was accepted")
	}
}

func TestDefaultReviewConfigRequiresServerCreationReview(t *testing.T) {
	if !defaultReviewConfig().ServerCreate {
		t.Fatal("server creation review must be enabled by default")
	}
}

func TestMergeServerModRequestsRetainsDetectedAndDeclaredEvidence(t *testing.T) {
	result := mergeServerModRequests(
		[]serverprobe.Mod{{ID: "Example_Mod", Version: "1.0", Source: "forge_status", Confidence: "exact"}},
		[]createServerModRequest{
			{ID: "example_mod"},
			{ID: "missing_mod"},
		},
	)
	if len(result) != 3 {
		t.Fatalf("got %d evidence records, want 3", len(result))
	}
	byIdentityAndSource := make(map[string]trustedServerModEvidence, len(result))
	for _, evidence := range result {
		byIdentityAndSource[evidence.ID+"/"+evidence.Source] = evidence
	}
	if detected := byIdentityAndSource["example_mod/forge_status"]; detected.Confidence != "exact" || detected.Version != "1.0" {
		t.Fatalf("detected evidence was overwritten: %#v", detected)
	}
	for _, key := range []string{"example_mod/manual", "missing_mod/manual"} {
		if declared := byIdentityAndSource[key]; declared.Confidence != "declared" {
			t.Fatalf("declaration %q did not retain independent manual evidence: %#v", key, declared)
		}
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
			{ID: "example_mod"},
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

func TestCatalogModFiltersNormalizesAndDeduplicates(t *testing.T) {
	filters := parseCatalogModFilters(" Example_Mod,example_mod,invalid:mod, second.mod ")
	if len(filters) != 2 || filters[0] != "example_mod" || filters[1] != "second.mod" {
		t.Fatalf("unexpected filters: %#v", filters)
	}
}
