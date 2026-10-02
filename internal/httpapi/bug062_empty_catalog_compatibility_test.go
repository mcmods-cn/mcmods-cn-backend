package httpapi

import "testing"

func TestDefaultMinecraftLoaderCompatibilityIsUnknownUntilVerified(t *testing.T) {
	config := defaultMinecraftVersionConfig()
	if len(config.Versions) == 0 || len(config.Loaders) == 0 {
		t.Fatal("safe default must retain the version catalog and loader choices")
	}
	for _, loader := range config.Loaders {
		if loader.Versions == nil {
			t.Fatalf("default loader %q versions are null; want an explicit empty range", loader.Code)
		}
		if len(loader.Versions) != 0 {
			t.Fatalf("default loader %q claims %d unverified compatible versions", loader.Code, len(loader.Versions))
		}
	}
}

func TestGlobalModContentCompatibilityPreservesVerifiedLoaderRanges(t *testing.T) {
	config := minecraftVersionConfig{
		Versions: []minecraftVersionOption{
			{Code: "1.20.1", Type: "release"},
			{Code: "24w14potato", Type: "april_fools"},
		},
		Loaders: []minecraftLoaderOption{
			{Code: "Forge", Name: "Forge", Versions: []string{"1.20.1"}},
			{Code: "Babric", Name: "Babric", Versions: []string{}},
		},
	}

	compatibilities := globalModContentCompatibilities(config)
	if len(compatibilities) != 2 {
		t.Fatalf("global compatibility count=%d; want 2", len(compatibilities))
	}
	if got := compatibilities[0]; got.Loader != "Forge" || len(got.Versions) != 1 || got.Versions[0] != "1.20.1" {
		t.Fatalf("Forge compatibility=%#v; want only its verified 1.20.1 range", got)
	}
	if got := compatibilities[1]; got.Loader != "Babric" || got.Versions == nil || len(got.Versions) != 0 {
		t.Fatalf("Babric compatibility=%#v; want an explicit unknown/empty range", got)
	}
}
