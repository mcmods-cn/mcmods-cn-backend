package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestFavoriteExportMinecraftVersionBoundaryUsesCatalogAuthority(t *testing.T) {
	exportSource, err := os.ReadFile("favorite_modpack_export.go")
	if err != nil {
		t.Fatal(err)
	}
	authoritySource, err := os.ReadFile("minecraft_loader_artifact_authority.go")
	if err != nil {
		t.Fatal(err)
	}
	versionSource, err := os.ReadFile("minecraft_version_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"validMinecraftVersionCode(request.MinecraftVersion)",
		"MODPACK_EXPORT_INVALID_MINECRAFT_VERSION",
		"minecraftVersionEnabledForLoader(config, minecraftVersion, loader)",
	} {
		if !strings.Contains(string(exportSource)+string(authoritySource), fragment) {
			t.Fatalf("favorite export version boundary is missing %q", fragment)
		}
	}
	for _, fragment := range []string{"minecraftVersionCodePattern", "len(value) <= 80", "validMinecraftVersionCode(version.Code)"} {
		if !strings.Contains(string(versionSource), fragment) {
			t.Fatalf("Minecraft version catalog validation is missing %q", fragment)
		}
	}
}

func TestFavoriteExportMinecraftVersionCodesAreBoundedAndLoaderEnabled(t *testing.T) {
	for _, value := range []string{"1.21.1", "25w10a", "23w13a_or_b", "3D Shareware v1.34", "1.RV-Pre1"} {
		if !validMinecraftVersionCode(value) {
			t.Errorf("known version code %q was rejected", value)
		}
	}
	for _, value := range []string{"", " 1.21.1", "../../1.21.1", "1.21.1/forge", "版本1.21.1", strings.Repeat("a", 81)} {
		if validMinecraftVersionCode(value) {
			t.Errorf("unsafe version code %q was accepted", value)
		}
	}
	config := minecraftVersionConfig{
		Versions: []minecraftVersionOption{{Code: "1.21.1", Type: "release"}, {Code: "1.20.1", Type: "release"}},
		Loaders: []minecraftLoaderOption{
			{Code: "Fabric", Versions: []string{"1.21.1"}},
			{Code: "Forge", Versions: []string{"1.20.1"}},
		},
	}
	if !minecraftVersionEnabledForLoader(config, "1.21.1", "fabric") {
		t.Fatal("enabled exact version was rejected")
	}
	for _, testCase := range [][2]string{{"1.20.1", "fabric"}, {"1.21.1", "forge"}, {"9.9.9", "fabric"}} {
		if minecraftVersionEnabledForLoader(config, testCase[0], testCase[1]) {
			t.Errorf("version/loader %q/%q was not enabled", testCase[0], testCase[1])
		}
	}
}
