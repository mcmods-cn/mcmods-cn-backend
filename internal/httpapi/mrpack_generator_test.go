package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"testing"
)

func validMRPackTestFile(name string) mrpackFile {
	return mrpackFile{
		Path: "mods/" + name,
		Hashes: map[string]string{
			"sha1":   "1111111111111111111111111111111111111111",
			"sha512": "22222222222222222222222222222222222222222222222222222222222222222222222222222222222222222222222222222222222222222222222222222222",
		},
		Env:       mrpackEnvironment{Client: "required", Server: "required"},
		Downloads: []string{"https://cdn.modrinth.com/data/project/versions/version/" + name}, FileSize: 123,
	}
}

func TestBuildMRPackUsesRootIndexAndLoaderDependency(t *testing.T) {
	result, err := buildMRPack(mrpackBuildInput{
		Name: "Test Pack", VersionID: "2026.08.18-1", MinecraftVersion: "1.21.1",
		Loader: "fabric", LoaderVersion: "0.16.14", Files: []mrpackFile{validMRPackTestFile("example.jar")},
	})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(result.Data), result.Size)
	if err != nil {
		t.Fatalf("result is not a ZIP archive: %v", err)
	}
	if len(archive.File) != 1 || archive.File[0].Name != modrinthIndexName {
		t.Fatalf("unexpected archive structure: %#v", archive.File)
	}
	reader, _ := archive.File[0].Open()
	defer reader.Close()
	var index mrpackIndex
	if err = json.NewDecoder(reader).Decode(&index); err != nil {
		t.Fatalf("index is not UTF-8 JSON: %v", err)
	}
	if index.FormatVersion != 1 || index.Game != "minecraft" || index.VersionID != "2026.08.18-1" {
		t.Fatalf("invalid index metadata: %#v", index)
	}
	if index.Dependencies["minecraft"] != "1.21.1" || index.Dependencies["fabric-loader"] != "0.16.14" {
		t.Fatalf("invalid dependencies: %#v", index.Dependencies)
	}
}

func TestBuildMRPackRejectsUnsafeOrIncompleteFiles(t *testing.T) {
	testCases := []struct {
		name string
		edit func(*mrpackFile)
	}{
		{"path traversal", func(file *mrpackFile) { file.Path = "mods/../evil.jar" }},
		{"missing sha512", func(file *mrpackFile) { delete(file.Hashes, "sha512") }},
		{"untrusted download", func(file *mrpackFile) { file.Downloads = []string{"https://example.com/mod.jar"} }},
		{"zero size", func(file *mrpackFile) { file.FileSize = 0 }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			file := validMRPackTestFile("example.jar")
			testCase.edit(&file)
			_, err := buildMRPack(mrpackBuildInput{Name: "Pack", VersionID: "1.0.0", MinecraftVersion: "1.21.1", Loader: "neoforge", LoaderVersion: "21.1.200", Files: []mrpackFile{file}})
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestMRPackLoaderDependencyKeys(t *testing.T) {
	wants := map[string]string{"fabric": "fabric-loader", "forge": "forge", "neoforge": "neoforge"}
	for loader, want := range wants {
		got, err := mrpackLoaderDependencyKey(loader)
		if err != nil || got != want {
			t.Fatalf("loader %s: got %q, %v", loader, got, err)
		}
	}
}
