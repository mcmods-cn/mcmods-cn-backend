package httpapi

import (
	"errors"
	"testing"
)

func TestNormalizeModContentSimilarGroups(t *testing.T) {
	t.Run("keeps a group in one category", func(t *testing.T) {
		resources := []modContentLayoutResourceEdit{
			{ResourcePublicID: "resource1", SectionPublicID: "section01", SimilarGroupID: "similar01"},
			{ResourcePublicID: "resource2", SectionPublicID: "section01", SimilarGroupID: "similar01"},
		}
		if err := normalizeModContentSimilarGroups(resources); err != nil {
			t.Fatalf("normalize group: %v", err)
		}
		if resources[0].SimilarGroupID != "similar01" || resources[1].SimilarGroupID != "similar01" {
			t.Fatalf("group was unexpectedly removed: %#v", resources)
		}
	})

	t.Run("removes a single member group", func(t *testing.T) {
		resources := []modContentLayoutResourceEdit{{ResourcePublicID: "resource1", SectionPublicID: "section01", SimilarGroupID: "similar01"}}
		if err := normalizeModContentSimilarGroups(resources); err != nil {
			t.Fatalf("normalize singleton: %v", err)
		}
		if resources[0].SimilarGroupID != "" {
			t.Fatalf("singleton group was retained: %#v", resources)
		}
	})

	t.Run("rejects a group spanning categories", func(t *testing.T) {
		resources := []modContentLayoutResourceEdit{
			{ResourcePublicID: "resource1", SectionPublicID: "section01", SimilarGroupID: "similar01"},
			{ResourcePublicID: "resource2", SectionPublicID: "section02", SimilarGroupID: "similar01"},
		}
		if err := normalizeModContentSimilarGroups(resources); !errors.Is(err, errCatalogEditorInvalid) {
			t.Fatalf("expected invalid catalog edit, got %v", err)
		}
	})
}
