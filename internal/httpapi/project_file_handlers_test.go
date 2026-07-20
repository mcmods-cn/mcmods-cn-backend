package httpapi

import "testing"

func TestNormalizeProjectFileType(t *testing.T) {
	tests := map[string]string{
		"mod":          "mod",
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

func TestMinecraftVersionLess(t *testing.T) {
	if !minecraftVersionLess("1.20.1", "1.21") {
		t.Fatal("expected 1.20.1 to sort before 1.21")
	}
	if minecraftVersionLess("1.21.1", "1.21") {
		t.Fatal("did not expect 1.21.1 to sort before 1.21")
	}
}
