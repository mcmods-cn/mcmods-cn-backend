package httpapi

import "testing"

func TestDefaultReviewConfigAutoApprovesEmptyModContentSections(t *testing.T) {
	config := defaultReviewConfig()
	if config.ModContentSectionCreate {
		t.Fatal("empty mod content section creation must be auto-approved by default")
	}
	if !config.CatalogCreate {
		t.Fatal("the dedicated section setting must not disable review for other catalog creation")
	}
	if modContentReviewRequired(config, modContentSnapshot{Kind: "section", Operation: "create"}) {
		t.Fatal("empty section creation should be auto-approved under the default config")
	}
	if !modContentReviewRequired(config, modContentSnapshot{Kind: "version", Operation: "create"}) {
		t.Fatal("other mod content creation should continue using catalog create review")
	}
	config.ModContentSectionCreate = true
	if !modContentReviewRequired(config, modContentSnapshot{Kind: "section", Operation: "create"}) {
		t.Fatal("the dedicated setting should send empty section creation to review")
	}
}

func TestNormalizeModContentVersionEditBuildsLabel(t *testing.T) {
	edit := modContentVersionEdit{
		Label:             "user supplied label is ignored",
		MinecraftVersions: []string{"1.21.1", "1.20.1"},
		Loaders:           []string{"NeoForge", "Forge"},
	}
	if err := normalizeModContentVersionEdit(&edit); err != nil {
		t.Fatal(err)
	}
	if edit.Label != "1.21.1, 1.20.1 / NeoForge, Forge" {
		t.Fatalf("unexpected generated label: %q", edit.Label)
	}
}

func TestModContentVersionSelectionMustMatchCompatibilityMatrix(t *testing.T) {
	compatibilities := []modLoaderCompatibilityPayload{
		{Loader: "Forge", Versions: []string{"1.20.1", "1.19.2"}},
		{Loader: "Fabric", Versions: []string{"1.20.1"}},
	}
	if !modContentVersionSelectionSupported(modContentVersionEdit{MinecraftVersions: []string{"1.20.1"}, Loaders: []string{"Forge", "Fabric"}}, compatibilities) {
		t.Fatal("a fully supported selection should be accepted")
	}
	if modContentVersionSelectionSupported(modContentVersionEdit{MinecraftVersions: []string{"1.20.1", "1.19.2"}, Loaders: []string{"Forge", "Fabric"}}, compatibilities) {
		t.Fatal("a selection containing an unsupported loader/version pair should be rejected")
	}
}

func TestGlobalModContentCompatibilityFallbackAllowsEveryConfiguredVersion(t *testing.T) {
	compatibilities := globalModContentCompatibilities(minecraftVersionConfig{
		Versions: []minecraftVersionOption{{Code: "1.21.1"}, {Code: "1.20.1"}},
		Loaders:  []minecraftLoaderOption{{Code: "Forge", Versions: []string{"1.20.1"}}, {Code: "Fabric"}},
	})
	selection := modContentVersionEdit{MinecraftVersions: []string{"1.21.1", "1.20.1"}, Loaders: []string{"Forge", "Fabric"}}
	if !modContentVersionSelectionSupported(selection, compatibilities) {
		t.Fatal("an unconfigured mod should allow all globally configured versions and loaders")
	}
}

func TestAdvancementSectionUsesLockedPresentation(t *testing.T) {
	if got := lockedModContentDisplayMode("advancement", "large", "compact"); got != "large" {
		t.Fatalf("advancement display mode must be locked: got %q", got)
	}
	if got := lockedModContentDisplayMode("item_block", "compact", "large"); got != "large" {
		t.Fatalf("ordinary section display mode should remain editable: got %q", got)
	}
}
