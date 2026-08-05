package httpapi

import "testing"

func TestParseSimpleProjectImportSources(t *testing.T) {
	tests := []struct {
		projectType string
		provider    string
		input       string
		reference   string
	}{
		{"plugin", "modrinth", "https://modrinth.com/plugin/luckperms", "luckperms"},
		{"plugin", "curseforge", "https://www.curseforge.com/minecraft/bukkit-plugins/worldedit", "worldedit"},
		{"map", "curseforge", "https://www.curseforge.com/minecraft/worlds/example-world", "example-world"},
		{"resource_pack", "modrinth", "https://modrinth.com/resourcepack/faithful", "faithful"},
		{"shader_pack", "curseforge", "https://www.curseforge.com/minecraft/shaders/bsl-shaders", "bsl-shaders"},
		{"datapack", "modrinth", "https://modrinth.com/datapack/example-data", "example-data"},
		{"addon", "modrinth", "https://modrinth.com/mod/example-addon", "example-addon"},
		{"addon", "curseforge", "https://www.curseforge.com/minecraft/mc-addons/example-addon", "example-addon"},
	}
	for _, test := range tests {
		provider, _, reference, err := parseProjectImportSource(test.projectType, test.provider, test.input)
		if err != nil {
			t.Fatalf("parse %s %s: %v", test.projectType, test.provider, err)
		}
		if provider != test.provider || reference != test.reference {
			t.Fatalf("parse %s %s = %q, %q", test.projectType, test.provider, provider, reference)
		}
	}
	if _, _, _, err := parseProjectImportSource("map", "modrinth", "https://modrinth.com/mod/example"); err == nil {
		t.Fatal("maps must not accept Modrinth imports")
	}
	if _, _, _, err := parseProjectImportSource("resource_pack", "curseforge", "https://www.curseforge.com/minecraft/worlds/example"); err == nil {
		t.Fatal("resource packs must reject CurseForge world URLs")
	}
}

func TestSimpleProjectExternalClassification(t *testing.T) {
	loaders := simpleProjectLoadersFromExternal("plugin", []string{"NeoForge", "Paper"})
	if len(loaders) != 2 || loaders[0] != "paper" || loaders[1] != "neoforge" {
		t.Fatalf("unexpected plugin loaders: %#v", loaders)
	}
	if containsSimpleImportOption(loaders, "forge") {
		t.Fatal("NeoForge must not also classify as Forge")
	}
	categories := simpleProjectCategoriesFromExternal("resource_pack", []string{"Traditional", "32x", "Mod Support"})
	if !containsSimpleImportOption(categories, "vanilla_like") || !containsSimpleImportOption(categories, "modded") {
		t.Fatalf("unexpected resource-pack categories: %#v", categories)
	}
	if resolution := simpleProjectResolutionFromExternal([]string{"Traditional", "32x"}); resolution != "32x" {
		t.Fatalf("resolution = %q", resolution)
	}
	versions := uniqueMinecraftVersions([]string{"1.21.1", "b1.7.3", "23w31a", "Forge", "Java 17"})
	if len(versions) != 3 || !containsSimpleImportOption(versions, "b1.7.3") || !containsSimpleImportOption(versions, "23w31a") {
		t.Fatalf("unexpected Minecraft versions: %#v", versions)
	}
}

func TestModrinthSimpleProjectTypeMatching(t *testing.T) {
	tests := []struct {
		name, projectType, providerType string
		loaders                         []string
		want                            bool
	}{
		{name: "native plugin type", projectType: "plugin", providerType: "plugin", want: true},
		{name: "legacy plugin type", projectType: "plugin", providerType: "mod", loaders: []string{"Paper"}, want: true},
		{name: "regular forge mod is not a plugin", projectType: "plugin", providerType: "mod", loaders: []string{"Forge"}},
		{name: "native data pack type", projectType: "datapack", providerType: "datapack", want: true},
		{name: "legacy data pack type", projectType: "datapack", providerType: "mod", loaders: []string{"datapack"}, want: true},
		{name: "resource pack is not a data pack", projectType: "datapack", providerType: "resourcepack", loaders: []string{"minecraft"}},
		{name: "plugin can be an add-on", projectType: "addon", providerType: "plugin", want: true},
		{name: "data pack can be an add-on", projectType: "addon", providerType: "datapack", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := modrinthProjectMatchesSimpleType(test.projectType, test.providerType, test.loaders); got != test.want {
				t.Fatalf("type match = %v, want %v", got, test.want)
			}
		})
	}
}

func TestExternalSourceLinkType(t *testing.T) {
	tests := map[string]string{
		"https://github.com/example/project":       "github",
		"https://gitlab.com/example/project":       "gitlab",
		"https://bitbucket.org/example/project":    "bitbucket",
		"https://code.example.net/example/project": "other",
	}
	for sourceURL, want := range tests {
		if got := externalSourceLink(sourceURL).Type; got != want {
			t.Fatalf("source link type for %q = %q, want %q", sourceURL, got, want)
		}
	}
}

func TestImportedSimpleProjectDraftsAreEditorReady(t *testing.T) {
	tests := []struct {
		projectType string
		values      []string
	}{
		{projectType: "plugin", values: []string{"Paper", "administration"}},
		{projectType: "map", values: []string{"adventure"}},
		{projectType: "resource_pack", values: []string{"32x", "models"}},
		{projectType: "shader_pack", values: []string{"Iris", "shadows"}},
		{projectType: "datapack", values: []string{"datapack", "world generation"}},
		{projectType: "addon", values: []string{"NeoForge"}},
	}
	for _, test := range tests {
		t.Run(test.projectType, func(t *testing.T) {
			draft := simpleProjectDraftFromExternal(simpleProjectImportData{
				ProjectType: test.projectType, Provider: "modrinth", ProviderURL: "https://modrinth.com/project/example",
				ProviderProjectID: "example", Slug: "example", Name: "Example", MinecraftVersions: []string{"1.21.1"},
				ExternalValues: test.values, Status: "active",
			})
			if err := normalizeAndValidateSimpleProjectDraft(&draft, true); err != nil {
				t.Fatalf("imported draft is not editor-ready: %v; draft=%#v", err, draft)
			}
		})
	}
}

func TestImportedAddonDraftAllowsParentSelectionAfterImport(t *testing.T) {
	draft := simpleProjectDraftFromExternal(simpleProjectImportData{
		ProjectType: "addon", Provider: "modrinth", ProviderURL: "https://modrinth.com/mod/example-addon",
		ProviderProjectID: "AABBCCDD", Slug: "example-addon", Name: "Example Add-on",
		MinecraftVersions: []string{"1.21.1"}, ExternalValues: []string{"NeoForge"},
	})
	if err := normalizeAndValidateSimpleProjectDraft(&draft, true); err != nil {
		t.Fatalf("import draft should allow selecting its parent later: %v", err)
	}
	if err := normalizeAndValidateSimpleProject(&draft); err == nil {
		t.Fatal("the final submitted add-on must still require a parent project")
	}
	if draft.ModrinthProjectID != "AABBCCDD" || len(draft.Links) != 1 || draft.Links[0].URL != "https://modrinth.com/mod/example-addon" {
		t.Fatalf("unexpected imported add-on identity: %#v", draft)
	}
}

func containsSimpleImportOption(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
