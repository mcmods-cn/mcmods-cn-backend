package httpapi

import "testing"

func TestCatalogResourceIdentityResolverUnifiesModIDAliases(t *testing.T) {
	resolver := catalogResourceIdentityResolver{
		"ae2":                 {ProjectCode: "abc234xyz", PrimaryIdentifier: "ae2"},
		"appliedenergistics2": {ProjectCode: "abc234xyz", PrimaryIdentifier: "ae2"},
	}
	modern := resolver.resolve("minecraft.item", "ae2:black1")
	legacy := resolver.resolve("minecraft.item", "AppliedEnergistics2:black1")
	if modern.ID != legacy.ID || modern.PublicID != legacy.PublicID {
		t.Fatalf("Mod ID aliases must share one global resource identity: modern=%+v legacy=%+v", modern, legacy)
	}
	if legacy.CanonicalID != "ae2:black1" || legacy.RawID != "AppliedEnergistics2:black1" {
		t.Fatalf("legacy alias was not canonicalized correctly: %+v", legacy)
	}
	block := resolver.resolve("minecraft.block", "AppliedEnergistics2:black1")
	if block.ID == legacy.ID {
		t.Fatal("different resource kinds must not share an identity")
	}
}
