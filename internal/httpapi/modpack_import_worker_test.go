package httpapi

import (
	"encoding/json"
	"testing"
)

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
