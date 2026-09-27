package httpapi

import (
	"reflect"
	"testing"
)

func TestNormalizeProjectFileType(t *testing.T) {
	tests := map[string]string{
		"mod":          "mod",
		"addon":        "addon",
		"resourcepack": "resource_pack",
		"shader-pack":  "shader_pack",
		"unknown":      "",
	}
	for input, expected := range tests {
		if actual := normalizeProjectFileType(input); actual != expected {
			t.Fatalf("normalizeProjectFileType(%q)=%q, want %q", input, actual, expected)
		}
	}
}

func TestProjectFileExtensionsForLargeResources(t *testing.T) {
	tests := []struct {
		projectType string
		extension   string
		allowed     bool
	}{
		{"plugin", ".jar", true},
		{"plugin", ".zip", false},
		{"map", ".zip", true},
		{"resource_pack", ".zip", true},
		{"shader_pack", ".zip", true},
		{"datapack", ".zip", true},
		{"addon", ".jar", true},
		{"addon", ".zip", true},
		{"addon", ".exe", false},
	}
	for _, test := range tests {
		if actual := projectFileExtensionAllowed(test.projectType, test.extension); actual != test.allowed {
			t.Fatalf("projectFileExtensionAllowed(%q,%q)=%v, want %v", test.projectType, test.extension, actual, test.allowed)
		}
	}
}

func TestSimpleProjectClassifications(t *testing.T) {
	if !validSimpleProjectOptions("shader_pack", []string{"iris"}, []string{"realistic"}, []string{"pbr", "shadows"}) {
		t.Fatal("expected supported shader classification to be valid")
	}
	if validSimpleProjectOptions("shader_pack", []string{"paper"}, []string{"realistic"}, nil) {
		t.Fatal("expected a plugin loader to be rejected for shader packs")
	}
	if validSimpleProjectOptions("resource_pack", nil, []string{"realistic"}, []string{"unknown_feature"}) {
		t.Fatal("expected an unknown resource-pack feature to be rejected")
	}
}

func TestCurseForgeFileCompatibility(t *testing.T) {
	loaders, versions := curseForgeFileCompatibility([]string{"1.21.1", "NeoForge", "Java 21", "1.20.1", "Forge"})
	if len(loaders) != 2 || loaders[0] != "NeoForge" || loaders[1] != "Forge" {
		t.Fatalf("unexpected loaders: %#v", loaders)
	}
	if len(versions) != 2 || versions[0] != "1.21.1" || versions[1] != "1.20.1" {
		t.Fatalf("unexpected versions: %#v", versions)
	}
}

func TestValidProviderDownloadURL(t *testing.T) {
	if !validProviderDownloadURL("https://cdn.modrinth.com/data/project/version/file.jar") {
		t.Fatal("expected HTTPS provider URL to be accepted")
	}
	for _, value := range []string{"http://cdn.example/file.jar", "file:///tmp/file.jar", "https://user:pass@example.com/file.jar", ""} {
		if validProviderDownloadURL(value) {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

func TestMinecraftVersionSortUsesAuthoritativeRegistry(t *testing.T) {
	codes := []string{"unknown-z", "1.21.1", "25w10a", "unknown-a"}
	sortMinecraftVersionCodes(codes, map[string]int{"25w10a": 0, "1.21.1": 1})
	want := []string{"25w10a", "1.21.1", "unknown-a", "unknown-z"}
	if !reflect.DeepEqual(codes, want) {
		t.Fatalf("Minecraft versions=%#v, want %#v", codes, want)
	}
}
