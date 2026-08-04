package httpapi

import (
	"regexp"
	"testing"
)

func TestModSiteIDBase(t *testing.T) {
	tests := map[string]string{
		"Create":                    "create",
		"Farmer's Delight":          "farmer_s_delight",
		" Just Enough Items (JEI) ": "just_enough_items_jei",
		"植物魔法":                      "mod",
	}
	for input, expected := range tests {
		if actual := modSiteIDBase(input); actual != expected {
			t.Fatalf("modSiteIDBase(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestValidModSiteID(t *testing.T) {
	for _, value := range []string{"create", "create_2", "create-fabric", "m1"} {
		if !validModSiteID(value) {
			t.Fatalf("valid site ID rejected: %q", value)
		}
	}
	for _, value := range []string{"", "Create", "_create", "create_", "机械动力", "create/2"} {
		if validModSiteID(value) {
			t.Fatalf("invalid site ID accepted: %q", value)
		}
	}
}

func TestNormalizeAndValidateModRequest(t *testing.T) {
	req := createModRequest{
		SiteID:          "create",
		PrimaryName:     " Create ",
		SecondaryName:   " 机械动力 ",
		Abbreviation:    "Create",
		Environment:     "bothRequired",
		PrimaryCategory: "technology",
		OfficialStatus:  "active",
		SourceStatus:    "partial",
		License:         "Custom",
		Tags:            []string{"automation", " automation ", "logistics"},
		SearchKeywords:  []string{"kinetic", " kinetic "},
		Links: []modLinkPayload{
			{Type: "modrinth", URL: "https://modrinth.com/mod/create"},
			{Type: "", URL: ""},
		},
		Authors: []modAuthorPayload{{Name: " simibubi ", AvatarURL: " https://cdn.modrinth.com/user/simibubi.png ", Role: "lead"}},
		Compatibilities: []modLoaderCompatibilityPayload{
			{Loader: "Forge", Versions: []string{"1.20.1", "1.19.2"}},
			{Loader: "Fabric", Versions: []string{"1.20.1"}},
		},
		RelationshipGroups: []modRelationshipGroupPayload{{
			Loader:            "Forge",
			MinecraftVersions: []string{"1.20.1"},
			Relationships:     []modRelationshipPayload{{Type: "dependency", RelatedModName: "Flywheel"}},
		}},
	}
	if err := normalizeAndValidateModRequest(&req); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if req.PrimaryName != "Create" || len(req.Tags) != 2 || len(req.SearchKeywords) != 1 || len(req.Links) != 1 || req.SubmissionMethod != "manual" {
		t.Fatalf("request was not normalized: %#v", req)
	}
	if len(req.Compatibilities) != 2 || len(req.Compatibilities[0].Versions) != 2 {
		t.Fatalf("compatibilities were not normalized: %#v", req.Compatibilities)
	}
	if req.Authors[0].AvatarURL != "https://cdn.modrinth.com/user/simibubi.png" {
		t.Fatalf("author avatar URL was not normalized: %q", req.Authors[0].AvatarURL)
	}
}

func TestNormalizeModRequestSynchronizesGitHubProjectLink(t *testing.T) {
	req := createModRequest{
		SiteID: "example", PrimaryName: "Example", Environment: "bothRequired", PrimaryCategory: "technology",
		OfficialStatus: "active", SourceStatus: "open", License: "MIT", SubmissionMethod: "manual",
		GitHubProjectPath: "FortyTwo/Test",
		Links:             []modLinkPayload{{Type: "official", URL: "https://example.com", Note: "Official documentation"}},
	}
	if err := normalizeAndValidateModRequest(&req); err != nil {
		t.Fatalf("valid GitHub project path rejected: %v", err)
	}
	if len(req.Links) != 2 || req.Links[1].Type != "github" || req.Links[1].URL != "https://github.com/FortyTwo/Test" {
		t.Fatalf("GitHub project link was not added: %#v", req.Links)
	}

	req.GitHubProjectPath = ""
	req.Links = []modLinkPayload{{Type: "github", URL: "https://github.com/OpenAI/codex/issues", Note: "Source"}}
	if err := normalizeAndValidateModRequest(&req); err != nil {
		t.Fatalf("valid GitHub link rejected: %v", err)
	}
	if req.GitHubProjectPath != "OpenAI/codex" || req.Links[0].URL != "https://github.com/OpenAI/codex" || req.Links[0].Note != "Source" {
		t.Fatalf("GitHub project path was not derived from the link: %#v / %#v", req.GitHubProjectPath, req.Links)
	}
}

func TestNormalizeModRequestAcceptsUncollectedRelationshipModID(t *testing.T) {
	req := createModRequest{
		SiteID: "example", PrimaryName: "Example", Environment: "bothRequired", PrimaryCategory: "technology",
		OfficialStatus: "active", SourceStatus: "open", License: "MIT", SubmissionMethod: "manual",
		RelationshipGroups: []modRelationshipGroupPayload{{
			Relationships: []modRelationshipPayload{{
				Type: "dependency", RelatedModIdentifier: " Future_Mod ",
			}},
		}},
	}
	if err := normalizeAndValidateModRequest(&req); err != nil {
		t.Fatalf("uncollected relationship Mod ID was rejected: %v", err)
	}
	got := req.RelationshipGroups[0].Relationships[0].RelatedModIdentifier
	if got != "Future_Mod" {
		t.Fatalf("uncollected relationship Mod ID was not normalized: %q", got)
	}
}

func TestFirstDefinitionStringListSupportsNestedResourceFields(t *testing.T) {
	definition := map[string]any{
		"enchanting": map[string]any{
			"compatible_enchantments": []any{"minecraft:mending", " example:future ", "minecraft:mending"},
		},
	}
	got := firstDefinitionStringList(definition,
		[]string{"enchanting", "compatible_enchantments"},
		[]string{"compatible_enchantments"},
	)
	if len(got) != 2 || got[0] != "minecraft:mending" || got[1] != "example:future" {
		t.Fatalf("unexpected definition references: %#v", got)
	}
}

func TestModIDVersionsMustUseSupportedVersions(t *testing.T) {
	req := createModRequest{
		SiteID: "example", PrimaryName: "Example", Environment: "bothRequired", PrimaryCategory: "technology",
		OfficialStatus: "active", SourceStatus: "open", License: "MIT", SubmissionMethod: "manual",
		ModIDs:          []modIdentifierPayload{{Identifier: "example", Primary: true, MinecraftVersions: []string{"1.19.2"}}},
		Compatibilities: []modLoaderCompatibilityPayload{{Loader: "Forge", Versions: []string{"1.20.1"}}},
	}
	if err := normalizeAndValidateModRequest(&req); err == nil {
		t.Fatal("unsupported Mod ID version was accepted")
	}
}

func TestNormalizeMinecraftVersionConfigFiltersUnsupportedLoaderVersions(t *testing.T) {
	config, err := normalizeMinecraftVersionConfig(minecraftVersionConfig{
		Versions: []minecraftVersionOption{{Code: "1.21.1", Type: "release"}},
		Loaders:  []minecraftLoaderOption{{Code: "Forge", Versions: []string{"1.21.1", "unknown"}}},
	})
	if err != nil {
		t.Fatalf("normalizeMinecraftVersionConfig() error = %v", err)
	}
	if len(config.Loaders) != 1 || len(config.Loaders[0].Versions) != 1 || config.Loaders[0].Versions[0] != "1.21.1" {
		t.Fatalf("unsupported versions were not filtered: %#v", config)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("Minecraft 模组评论", 9); got != "Minecraft…" {
		t.Fatalf("truncateRunes() = %q", got)
	}
}

func TestRejectInvalidModRequest(t *testing.T) {
	base := createModRequest{
		SiteID:           "example",
		PrimaryName:      "Example",
		Environment:      "bothRequired",
		PrimaryCategory:  "technology",
		OfficialStatus:   "active",
		SourceStatus:     "open",
		License:          "MIT",
		SubmissionMethod: "manual",
	}
	tests := []struct {
		name   string
		mutate func(*createModRequest)
	}{
		{name: "unicode abbreviation", mutate: func(req *createModRequest) { req.Abbreviation = "科技" }},
		{name: "invalid category", mutate: func(req *createModRequest) { req.PrimaryCategory = "anything" }},
		{name: "invalid link", mutate: func(req *createModRequest) {
			req.Links = []modLinkPayload{{Type: "github", URL: "javascript:alert(1)"}}
		}},
		{name: "relationship without target", mutate: func(req *createModRequest) {
			req.RelationshipGroups = []modRelationshipGroupPayload{{Relationships: []modRelationshipPayload{{Type: "dependency"}}}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := base
			test.mutate(&req)
			if err := normalizeAndValidateModRequest(&req); err == nil {
				t.Fatal("invalid request was accepted")
			}
		})
	}
}

func TestChangedSnapshotFieldsIncludesStructuredContent(t *testing.T) {
	before := createModRequest{SiteID: "create", PrimaryName: "Create", BodyMarkdown: "old"}
	after := before
	after.SiteID = "create_mod"
	after.BodyMarkdown = "new"
	after.RelationshipGroups = []modRelationshipGroupPayload{{
		Loader:            "Forge",
		MinecraftVersions: []string{"1.20.1"},
		Relationships:     []modRelationshipPayload{{Type: "dependency", RelatedModName: "Flywheel"}},
	}}
	changed := topLevelChangedFields(revisionChanges(before, after))
	if len(changed) != 3 || changed[0] != "bodyMarkdown" || changed[1] != "relationshipGroups" || changed[2] != "siteId" {
		t.Fatalf("unexpected changed fields: %#v", changed)
	}
}

func TestConcreteProjectRole(t *testing.T) {
	role, ok := concreteProjectRole("project_editor.[ProjectID]", "a2bc3de")
	if !ok || role != "project_editor.a2bc3de" {
		t.Fatalf("concreteProjectRole() = %q, %v", role, ok)
	}
	if _, ok = concreteProjectRole("system_admin", "a2bc3de"); ok {
		t.Fatal("non-variable permission group was accepted as a project role")
	}
}

func TestNewModUniqueID(t *testing.T) {
	uniqueID, err := newModUniqueID()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[a-z][2-9][a-z2-9]{7}$`).MatchString(uniqueID) {
		t.Fatalf("newModUniqueID() = %q", uniqueID)
	}
}

func TestDefaultMinecraftVersionsContainOptionalTypes(t *testing.T) {
	config := defaultMinecraftVersionConfig()
	types := map[string]bool{}
	for _, version := range config.Versions {
		types[version.Type] = true
	}
	for _, required := range []string{"release", "snapshot", "april_fools", "legacy"} {
		if !types[required] {
			t.Fatalf("default Minecraft versions do not contain %s", required)
		}
	}
}
