package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
	if len(loaders) != 1 || loaders[0] != "paper" {
		t.Fatalf("unexpected plugin loaders: %#v", loaders)
	}
	if containsSimpleImportOption(loaders, "forge") || containsSimpleImportOption(loaders, "neoforge") || containsSimpleImportOption(loaders, "fabric") {
		t.Fatal("mod loaders must not be classified as plugin platforms")
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

func TestSimpleProjectProviderFreeTextDoesNotCreateStructuredClassification(t *testing.T) {
	t.Run("modrinth", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			switch request.URL.Path {
			case "/project/example":
				_, _ = response.Write([]byte(`{"id":"project-id","slug":"example","title":"Paper City 32x","description":"A path tracing model pack","body":"Includes 32x models and city textures","project_type":"resourcepack","categories":["traditional"],"game_versions":["1.21.1"],"status":"approved","license":{"id":"MIT"}}`))
			case "/project/project-id/version":
				_, _ = response.Write([]byte(`[]`))
			default:
				http.NotFound(response, request)
			}
		}))
		defer server.Close()

		cfg := defaultModImportConfig()
		cfg.Modrinth.BaseURL = server.URL
		draft, err := importModrinthSimpleProject(context.Background(), server.Client(), cfg, "resource_pack", "https://modrinth.com/resourcepack/example", "example")
		if err != nil {
			t.Fatal(err)
		}
		assertFreeTextDidNotClassifyResourcePack(t, draft)
	})

	t.Run("curseforge", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			switch request.URL.Path {
			case "/categories":
				_, _ = response.Write([]byte(`{"data":[{"id":12,"slug":"texture-packs","isClass":true}]}`))
			case "/mods/search":
				_, _ = response.Write([]byte(`{"data":[{"id":34,"classId":12,"slug":"example","name":"Paper City 32x","summary":"A path tracing model pack","isAvailable":true,"categories":[{"name":"Traditional","slug":"traditional"}],"latestFilesIndexes":[{"gameVersion":"1.21.1"}]}]}`))
			case "/mods/34/description":
				_, _ = response.Write([]byte(`{"data":"<p>Includes 32x models and city textures</p>"}`))
			default:
				http.NotFound(response, request)
			}
		}))
		defer server.Close()

		cfg := defaultModImportConfig()
		cfg.CurseForge.BaseURL = server.URL
		cfg.CurseForge.APIKey = ""
		draft, err := importCurseForgeSimpleProject(context.Background(), server.Client(), cfg, "resource_pack", "https://www.curseforge.com/minecraft/texture-packs/example", "example")
		if err != nil {
			t.Fatal(err)
		}
		assertFreeTextDidNotClassifyResourcePack(t, draft)
	})
}

func TestSimpleProjectSecondaryMetadataFailuresAreVisible(t *testing.T) {
	t.Run("modrinth versions", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			if request.URL.Path == "/project/example" {
				_, _ = response.Write([]byte(`{"id":"project-id","slug":"example","title":"Example","project_type":"resourcepack","status":"approved","license":{"id":"MIT"}}`))
				return
			}
			http.Error(response, "failed", http.StatusInternalServerError)
		}))
		defer server.Close()
		cfg := defaultModImportConfig()
		cfg.Modrinth.BaseURL = server.URL
		if _, err := importModrinthSimpleProject(context.Background(), server.Client(), cfg, "resource_pack", "https://modrinth.com/resourcepack/example", "example"); err == nil {
			t.Fatal("version failure was ignored")
		}
	})

	t.Run("modrinth team", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			switch request.URL.Path {
			case "/project/example":
				_, _ = response.Write([]byte(`{"id":"project-id","slug":"example","title":"Example","project_type":"resourcepack","team":"team-id","status":"approved","license":{"id":"MIT"}}`))
			case "/project/project-id/version":
				_, _ = response.Write([]byte(`[]`))
			default:
				http.Error(response, "failed", http.StatusTooManyRequests)
			}
		}))
		defer server.Close()
		cfg := defaultModImportConfig()
		cfg.Modrinth.BaseURL = server.URL
		if _, err := importModrinthSimpleProject(context.Background(), server.Client(), cfg, "resource_pack", "https://modrinth.com/resourcepack/example", "example"); err == nil {
			t.Fatal("team failure was ignored")
		}
	})

	t.Run("curseforge description", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			switch request.URL.Path {
			case "/categories":
				_, _ = response.Write([]byte(`{"data":[{"id":12,"slug":"texture-packs","isClass":true}]}`))
			case "/mods/search":
				_, _ = response.Write([]byte(`{"data":[{"id":34,"classId":12,"slug":"example","name":"Example","isAvailable":true}]}`))
			default:
				http.Error(response, "failed", http.StatusInternalServerError)
			}
		}))
		defer server.Close()
		cfg := defaultModImportConfig()
		cfg.CurseForge.BaseURL = server.URL
		cfg.CurseForge.APIKey = ""
		if _, err := importCurseForgeSimpleProject(context.Background(), server.Client(), cfg, "resource_pack", "https://www.curseforge.com/minecraft/texture-packs/example", "example"); err == nil {
			t.Fatal("description failure was ignored")
		}
	})
}

func assertFreeTextDidNotClassifyResourcePack(t *testing.T, draft simpleProjectSnapshot) {
	t.Helper()
	if draft.Resolution != "16x" {
		t.Fatalf("free text changed resolution to %q", draft.Resolution)
	}
	if len(draft.Features) != 0 {
		t.Fatalf("free text created features: %#v", draft.Features)
	}
	if !containsSimpleImportOption(draft.Categories, "vanilla_like") {
		t.Fatalf("structured provider category was lost: %#v", draft.Categories)
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
				StructuredValues: test.values, Status: "active",
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
		MinecraftVersions: []string{"1.21.1"}, StructuredValues: []string{"NeoForge"},
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
