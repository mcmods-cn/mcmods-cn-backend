package httpapi

import "testing"

func TestMinecraftLoaderCodesAreCanonicalAndCaseInsensitiveUnique(t *testing.T) {
	config, err := normalizeMinecraftVersionConfig(minecraftVersionConfig{
		Versions: []minecraftVersionOption{{Code: "1.21.1", Type: "release"}},
		Loaders: []minecraftLoaderOption{
			{Code: " fOrGe ", Name: "Forge display", Versions: []string{"1.21.1"}},
			{Code: "MyCustomLoader", Name: "Custom display", Versions: []string{"1.21.1"}},
		},
		LoaderSyncs: []minecraftLoaderSyncStatus{{Code: "fORge", Status: "synced", VersionCount: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Loaders) != 2 || config.Loaders[0].Code != "Forge" || config.Loaders[1].Code != "mycustomloader" {
		t.Fatalf("canonical loader codes=%#v", config.Loaders)
	}
	if len(config.LoaderSyncs) != 1 || config.LoaderSyncs[0].Code != "Forge" {
		t.Fatalf("canonical loader sync codes=%#v", config.LoaderSyncs)
	}

	_, err = normalizeMinecraftVersionConfig(minecraftVersionConfig{
		Versions: []minecraftVersionOption{{Code: "1.21.1", Type: "release"}},
		Loaders: []minecraftLoaderOption{
			{Code: "Forge", Versions: []string{"1.21.1"}},
			{Code: "forge", Versions: []string{}},
		},
	})
	if err == nil {
		t.Fatal("case-insensitive duplicate loader codes were accepted")
	}
}
