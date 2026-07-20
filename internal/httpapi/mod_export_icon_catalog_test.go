package httpapi

import "testing"

func TestCollectExportIconCatalogCandidates(t *testing.T) {
	media := []modExportPNGMedia{
		{RevisionID: "revision-a", AssetPath: "icons/mob_effects/256/examplemod/charged/speed.png"},
		{RevisionID: "revision-a", AssetPath: "icons/mob_effects/32/examplemod/charged/speed.png"},
		{RevisionID: "revision-a", AssetPath: "icons/items/32/examplemod/ignored.png"},
	}
	items := collectExportIconCatalogCandidates(media)
	if len(items) != 1 {
		t.Fatalf("got %d normalized icon resources, want 1", len(items))
	}
	item := items[0]
	if item.Registry != "mob_effects" || item.CanonicalID != "examplemod:charged/speed" || item.TranslationKey != "effect.examplemod.charged.speed" {
		t.Fatalf("unexpected normalized icon identity: %#v", item)
	}
	if item.IconPath != "icons/mob_effects/32/examplemod/charged/speed.png" || item.PreviewPath != "icons/mob_effects/256/examplemod/charged/speed.png" {
		t.Fatalf("unexpected normalized icon media selection: %#v", item)
	}
}
