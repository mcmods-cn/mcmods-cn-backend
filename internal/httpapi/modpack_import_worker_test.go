package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTEST035ModpackArchiveCentralDirectoryIsBounded(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "too-many-central-directory-entries.mrpack")
	output, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(output)
	index, err := archive.Create("modrinth.index.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = index.Write([]byte(`{"formatVersion":1,"game":"minecraft","versionId":"test035","name":"TEST-035","files":[]}`)); err != nil {
		t.Fatal(err)
	}
	for entry := 0; entry < 4097; entry++ {
		if _, err = archive.Create("overrides/test035-" + strconv.Itoa(entry)); err != nil {
			t.Fatal(err)
		}
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err = output.Close(); err != nil {
		t.Fatal(err)
	}
	var parsed modrinthPackIndex
	if err = readJSONFromArchive(archivePath, []string{"modrinth.index.json"}, &parsed); err == nil || !strings.Contains(err.Error(), "central directory") {
		t.Fatalf("unbounded central directory result=%v", err)
	}
}

func TestTEST035ModpackIndexIsBounded(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "oversized-index.mrpack")
	output, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(output)
	index, err := archive.Create("modrinth.index.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = index.Write([]byte(strings.Repeat(" ", int(maxModpackIndexBytes+1)))); err != nil {
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err = output.Close(); err != nil {
		t.Fatal(err)
	}
	var parsed modrinthPackIndex
	if err = readJSONFromArchive(archivePath, []string{"modrinth.index.json"}, &parsed); err == nil || !strings.Contains(err.Error(), "index is too large") {
		t.Fatalf("oversized index result=%v", err)
	}
}

func TestModrinthIndexModsExtractsProviderReferences(t *testing.T) {
	var index modrinthPackIndex
	const raw = `{
		"game":"minecraft",
		"formatVersion":1,
		"versionId":"2.3",
		"name":"Zombie Invade 100 Days",
		"files":[
			{"path":"mods/gravestone-forge-1.20.1-1.0.35.jar","env":{"client":"required","server":"required"},"downloads":["https://cdn.modrinth.com/data/RYtXKJPr/versions/q9kZE5Xo/gravestone-forge-1.20.1-1.0.35.jar"]},
			{"path":"mods/local-only.jar","env":{"client":"required","server":"unsupported"},"downloads":[]},
			{"path":"resourcepacks/example.zip","env":{"client":"required","server":"unsupported"},"downloads":[]}
		],
		"dependencies":{"forge":"47.4.10","minecraft":"1.20.1"}
	}`
	if err := json.Unmarshal([]byte(raw), &index); err != nil {
		t.Fatalf("unmarshal index: %v", err)
	}

	mods := modrinthIndexMods(index)
	if len(mods) != 2 {
		t.Fatalf("expected 2 mod entries, got %d", len(mods))
	}
	if mods[0].ProviderProjectID != "RYtXKJPr" || mods[0].ProviderVersionID != "q9kZE5Xo" {
		t.Fatalf("unexpected provider references: %#v", mods[0])
	}
	if !mods[0].ClientRequired || !mods[0].ServerRequired {
		t.Fatalf("required sides were not retained: %#v", mods[0])
	}
	if mods[1].Identifier != "local-only" || !mods[1].ClientRequired || mods[1].ServerRequired {
		t.Fatalf("filename fallback or side metadata is wrong: %#v", mods[1])
	}

	loaders, versions := modrinthPackCompatibility(modrinthProject{}, modrinthVersion{}, index)
	if len(loaders) != 1 || loaders[0] != "Forge" || len(versions) != 1 || versions[0] != "1.20.1" {
		t.Fatalf("unexpected compatibility: loaders=%v versions=%v", loaders, versions)
	}
}

func TestModrinthDownloadIdentityRequiresOfficialConsistentCDNPaths(t *testing.T) {
	projectID, versionID, ok := modrinthDownloadIdentity("https://cdn.modrinth.com/data/RYtXKJPr/versions/q9kZE5Xo/mod.jar")
	if !ok || projectID != "RYtXKJPr" || versionID != "q9kZE5Xo" {
		t.Fatalf("official identity = %q %q %t", projectID, versionID, ok)
	}
	for _, rawURL := range []string{
		"https://attacker.example/data/RYtXKJPr/versions/q9kZE5Xo/mod.jar",
		"https://cdn.modrinth.com.evil.example/data/RYtXKJPr/versions/q9kZE5Xo/mod.jar",
		"http://cdn.modrinth.com/data/RYtXKJPr/versions/q9kZE5Xo/mod.jar",
		"https://user:secret@cdn.modrinth.com/data/RYtXKJPr/versions/q9kZE5Xo/mod.jar",
		"https://cdn.modrinth.com:444/data/RYtXKJPr/versions/q9kZE5Xo/mod.jar",
		"https://cdn.modrinth.com/file.jar?source=/data/RYtXKJPr/versions/q9kZE5Xo/mod.jar",
		"https://cdn.modrinth.com/data/RYtXKJPr/versions/q9kZE5Xo/mod.jar#fragment",
		"https://cdn.modrinth.com/data/RYtXKJPr/versions/q9kZE5Xo/nested/mod.jar",
	} {
		if projectID, versionID, ok = modrinthDownloadIdentity(rawURL); ok {
			t.Fatalf("untrusted URL %q produced identity %q/%q", rawURL, projectID, versionID)
		}
	}
	if _, _, ok = consistentModrinthDownloadIdentity([]string{
		"https://cdn.modrinth.com/data/project-a/versions/version-a/mod.jar",
		"https://cdn.modrinth.com/data/project-b/versions/version-b/mod.jar",
	}); ok {
		t.Fatal("conflicting mirror identities were accepted")
	}
}

func TestModrinthIndexModsDoesNotBindIdentityFromUntrustedURL(t *testing.T) {
	var index modrinthPackIndex
	index.Files = append(index.Files, struct {
		Path      string   `json:"path"`
		Downloads []string `json:"downloads"`
		Env       struct {
			Client string `json:"client"`
			Server string `json:"server"`
		} `json:"env"`
	}{
		Path:      "mods/victim.jar",
		Downloads: []string{"https://attacker.example/data/victim/versions/fake/victim.jar"},
	})
	mods := modrinthIndexMods(index)
	if len(mods) != 1 || mods[0].ProviderProjectID != "" || mods[0].ProviderVersionID != "" || mods[0].Identifier != "victim" {
		t.Fatalf("untrusted download created provider identity: %#v", mods)
	}
}

func TestParseModpackImportSource(t *testing.T) {
	tests := []struct {
		provider string
		input    string
		wantRef  string
	}{
		{provider: "modrinth", input: "https://modrinth.com/modpack/example-pack", wantRef: "example-pack"},
		{provider: "curseforge", input: "https://www.curseforge.com/minecraft/modpacks/example-pack", wantRef: "example-pack"},
	}
	for _, test := range tests {
		t.Run(test.provider, func(t *testing.T) {
			_, _, reference, err := parseProjectImportSource("modpack", test.provider, test.input)
			if err != nil {
				t.Fatalf("parse source: %v", err)
			}
			if reference != test.wantRef {
				t.Fatalf("expected reference %q, got %q", test.wantRef, reference)
			}
		})
	}
	if _, _, _, err := parseProjectImportSource("modpack", "modrinth", "https://modrinth.com/mod/example-mod"); err == nil {
		t.Fatal("a mod URL must not be accepted as a modpack URL")
	}
}

func TestExternalModpackCategoriesUseSiteTaxonomy(t *testing.T) {
	categories := modpackCategoriesFromExternal([]string{"Technology", "Questing", "Skyblock", "Expert"})
	wants := map[string]bool{"technology": true, "quests": true, "skyblock": true, "hardcore": true}
	for _, category := range categories {
		delete(wants, category)
	}
	if len(wants) != 0 {
		t.Fatalf("missing normalized categories: %#v (got %#v)", wants, categories)
	}
	if modpackTypeFromExternal([]string{"Expert progression"}) != "customized" {
		t.Fatal("expert packs should import as customized modpacks")
	}
}

func TestModpackSecondaryMetadataFailuresAreVisible(t *testing.T) {
	t.Run("modrinth authors", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			switch request.URL.Path {
			case "/project/example":
				_, _ = response.Write([]byte(`{"id":"project-id","project_type":"modpack","team":"team-id"}`))
			case "/project/project-id/version":
				_, _ = response.Write([]byte(`[]`))
			default:
				http.Error(response, "failed", http.StatusTooManyRequests)
			}
		}))
		defer server.Close()
		cfg := defaultModImportConfig()
		cfg.Modrinth.BaseURL = server.URL
		if _, err := loadModrinthProviderSnapshot(context.Background(), server.Client(), cfg, "example"); err == nil {
			t.Fatal("author failure was ignored")
		}
	})

	t.Run("curseforge description", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			if request.URL.Path == "/mods/search" {
				_, _ = response.Write([]byte(`{"data":[{"id":34,"classId":4471,"slug":"example","name":"Example","isAvailable":true}]}`))
				return
			}
			http.Error(response, "failed", http.StatusInternalServerError)
		}))
		defer server.Close()
		cfg := defaultModImportConfig()
		cfg.CurseForge.BaseURL = server.URL
		cfg.CurseForge.APIKey = ""
		if _, err := importCurseForgeModpack(context.Background(), server.Client(), cfg, "example"); err == nil {
			t.Fatal("description failure was ignored")
		}
	})
}

func TestExternalModpackFileSelectionIsReleasePrimaryAndOrderIndependent(t *testing.T) {
	oldRelease := modrinthVersion{
		ID: "release-old", Name: "Old release", VersionType: "release", Status: "listed",
		DatePublished: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Files:         []modrinthVersionFile{{URL: "https://cdn.modrinth.com/data/project/versions/release-old/old.mrpack", Filename: "old.mrpack", Primary: true}},
	}
	newRelease := modrinthVersion{
		ID: "release-new", Name: "New release", VersionType: "release", Status: "listed",
		DatePublished: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		Files: []modrinthVersionFile{
			{URL: "https://cdn.modrinth.com/data/project/versions/release-new/server.mrpack", Filename: "server.mrpack"},
			{URL: "https://cdn.modrinth.com/data/project/versions/release-new/main.mrpack", Filename: "main.mrpack", Primary: true},
		},
	}
	beta := modrinthVersion{
		ID: "beta", VersionType: "beta", Status: "listed", DatePublished: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		Files: []modrinthVersionFile{{URL: "https://cdn.modrinth.com/data/project/versions/beta/beta.mrpack", Filename: "beta.mrpack", Primary: true}},
	}
	for _, versions := range [][]modrinthVersion{{oldRelease, beta, newRelease}, {newRelease, oldRelease, beta}} {
		version, file, err := selectModrinthPackArchive(versions)
		if err != nil {
			t.Fatal(err)
		}
		if version.ID != "release-new" || file.Filename != "main.mrpack" {
			t.Fatalf("selected Modrinth %q/%q", version.ID, file.Filename)
		}
	}

	oldFile := curseForgePackFile{ID: 10, DisplayName: "Old", FileName: "old.zip", ReleaseType: 1, FileStatus: 4, IsAvailable: true, FileDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	newFile := curseForgePackFile{ID: 20, DisplayName: "New", FileName: "new.zip", ReleaseType: 1, FileStatus: 4, IsAvailable: true, FileDate: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}
	betaFile := curseForgePackFile{ID: 30, FileName: "beta.zip", ReleaseType: 2, FileStatus: 4, IsAvailable: true, FileDate: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}
	serverFile := curseForgePackFile{ID: 40, FileName: "server.zip", ReleaseType: 1, FileStatus: 4, IsAvailable: true, IsServerPack: true, FileDate: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)}
	for _, files := range [][]curseForgePackFile{{oldFile, betaFile, serverFile, newFile}, {newFile, serverFile, oldFile, betaFile}} {
		selected, err := selectCurseForgePackFile(files)
		if err != nil {
			t.Fatal(err)
		}
		if selected.ID != newFile.ID {
			t.Fatalf("selected CurseForge file %d", selected.ID)
		}
	}
}

func TestExternalModpackLocaleMustBeConfirmedBeforeSubmission(t *testing.T) {
	request := createModpackRequest{
		PrimaryName: "Example", DefaultLocale: externalModpackLocale, Environment: "bothRequired", PrimaryCategory: "adventure",
		PackType: "native", PackagingMethod: "modrinth", OfficialStatus: "active", SourceStatus: "open", License: "MIT",
		SubmissionMethod: "modrinth", Compatibilities: []modLoaderCompatibilityPayload{{Loader: "Fabric", Versions: []string{"1.21.1"}}},
		ImportSelection: &modpackImportSelection{
			Provider: "modrinth", ProjectID: "project", VersionID: "version", VersionName: "1.0.0", FileName: "pack.mrpack",
			ReleaseType: "release", PublishedAt: "2026-02-01T00:00:00Z",
		},
	}
	if err := normalizeAndValidateModpackImportDraft(&request); err != nil {
		t.Fatalf("undetermined import draft was rejected: %v", err)
	}
	if request.ImportSelection == nil {
		t.Fatal("import selection was removed from the job result")
	}
	if err := normalizeAndValidateModpackRequest(&request); err == nil {
		t.Fatal("undetermined source language was accepted as a final submission")
	}
	if request.ImportSelection != nil {
		t.Fatal("read-only import selection was retained in a final submission")
	}
}
