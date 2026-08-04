package httpapi

import "testing"

func TestModContentSnapshotAggregateKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		snapshot modContentSnapshot
		want     string
	}{
		{name: "version", snapshot: modContentSnapshot{Kind: "version", PublicID: "abc123456"}, want: "abc123456"},
		{name: "section", snapshot: modContentSnapshot{Kind: "section", PublicID: "def123456"}, want: "def123456"},
		{name: "layout", snapshot: modContentSnapshot{Kind: "layout", PublicID: "ghi123456"}, want: "ghi123456"},
		{
			name: "resource version",
			snapshot: modContentSnapshot{
				Kind:     "resource",
				PublicID: "jkl123456",
				Resource: &modContentResourceEdit{VersionPublicID: "mno123456"},
			},
			want: "jkl123456:mno123456",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := modContentSnapshotAggregateKey(test.snapshot); got != test.want {
				t.Fatalf("modContentSnapshotAggregateKey() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCanonicalizeModContentLayoutResourcesPrefersBoundBlock(t *testing.T) {
	t.Parallel()
	resources := []modContentLayoutResourceEdit{
		{ResourcePublicID: "itm123456", SectionPublicID: "items1234", Ordinal: 0},
		{ResourcePublicID: "blk123456", SectionPublicID: "blocks123", Ordinal: 0},
		{ResourcePublicID: "itm654321", SectionPublicID: "items1234", Ordinal: 1},
	}
	identities := map[string]modContentLayoutResourceIdentity{
		"itm123456": {
			KindCode: "minecraft.item", CanonicalID: "example:machine_item",
			BlockRepresentativeID: 42, BlockRepresentativePublicID: "blk123456",
		},
		"blk123456": {
			KindCode: "minecraft.block", CanonicalID: "example:machine_block",
			BlockRepresentativeID: 42, BlockRepresentativePublicID: "blk123456",
		},
		"itm654321": {KindCode: "minecraft.item", CanonicalID: "example:wrench"},
	}
	got := canonicalizeModContentLayoutResources(resources, identities)
	if len(got) != 2 {
		t.Fatalf("canonicalized resources = %#v, want two logical entries", got)
	}
	if got[0].ResourcePublicID != "blk123456" || got[0].SectionPublicID != "blocks123" {
		t.Fatalf("bound block did not replace its item alias: %#v", got[0])
	}
	if got[1].ResourcePublicID != "itm654321" {
		t.Fatalf("independent item was removed: %#v", got[1])
	}
}

func TestCanonicalizeModContentLayoutResourcesRewritesMappedItemOnly(t *testing.T) {
	t.Parallel()
	resources := []modContentLayoutResourceEdit{{
		ResourcePublicID: "itm123456",
		SectionPublicID:  "items1234",
		Ordinal:          3,
	}}
	identities := map[string]modContentLayoutResourceIdentity{
		"itm123456": {
			KindCode: "minecraft.item", CanonicalID: "example:machine_item",
			BlockRepresentativeID: 42, BlockRepresentativePublicID: "blk123456",
		},
	}
	got := canonicalizeModContentLayoutResources(resources, identities)
	if len(got) != 1 || got[0].ResourcePublicID != "blk123456" ||
		got[0].SectionPublicID != "items1234" || got[0].Ordinal != 3 {
		t.Fatalf("mapped item-only layout was not rewritten to its block representative: %#v", got)
	}
}

func TestCanonicalizeModContentLayoutResourcesFallsBackToCanonicalID(t *testing.T) {
	t.Parallel()
	resources := []modContentLayoutResourceEdit{
		{ResourcePublicID: "itm123456", SectionPublicID: "items1234"},
		{ResourcePublicID: "blk123456", SectionPublicID: "blocks123"},
	}
	identities := map[string]modContentLayoutResourceIdentity{
		"itm123456": {KindCode: "minecraft.item", CanonicalID: "example:machine"},
		"blk123456": {KindCode: "minecraft.block", CanonicalID: "example:machine"},
	}
	got := canonicalizeModContentLayoutResources(resources, identities)
	if len(got) != 1 || got[0].ResourcePublicID != "blk123456" {
		t.Fatalf("canonical item/block duplicate was not collapsed to the block: %#v", got)
	}
}

func TestValidateModContentAdvancementLayoutResolvesParents(t *testing.T) {
	t.Parallel()
	resources := []modContentLayoutResourceEdit{
		{
			ResourcePublicID: "adv123456",
			Advancement:      &modContentAdvancementLayoutEdit{GroupID: "advancement:example", X: 1, Y: 2},
		},
		{
			ResourcePublicID: "adv654321",
			Advancement: &modContentAdvancementLayoutEdit{
				ParentResourcePublicID: "adv123456",
				GroupID:                "advancement:example",
				X:                      3,
				Y:                      4,
			},
		},
	}
	identities := map[string]modContentLayoutResourceIdentity{
		"adv123456": {KindCode: "minecraft.advancement", CanonicalID: "example:root"},
		"adv654321": {KindCode: "minecraft.advancement", CanonicalID: "example:child"},
	}
	if err := validateModContentAdvancementLayout(resources, identities); err != nil {
		t.Fatalf("validateModContentAdvancementLayout() error = %v", err)
	}
}

func TestValidateModContentAdvancementLayoutRejectsCycles(t *testing.T) {
	t.Parallel()
	resources := []modContentLayoutResourceEdit{
		{
			ResourcePublicID: "adv123456",
			Advancement:      &modContentAdvancementLayoutEdit{ParentResourcePublicID: "adv654321", GroupID: "advancement:example"},
		},
		{
			ResourcePublicID: "adv654321",
			Advancement:      &modContentAdvancementLayoutEdit{ParentResourcePublicID: "adv123456", GroupID: "advancement:example"},
		},
	}
	identities := map[string]modContentLayoutResourceIdentity{
		"adv123456": {KindCode: "minecraft.advancement", CanonicalID: "example:root"},
		"adv654321": {KindCode: "minecraft.advancement", CanonicalID: "example:child"},
	}
	if err := validateModContentAdvancementLayout(resources, identities); err == nil {
		t.Fatal("validateModContentAdvancementLayout() accepted a cycle")
	}
}

func TestModContentResourceLocalizationProvenance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		existing  string
		unchanged bool
		want      string
	}{
		{name: "unchanged AI translation", existing: "ai", unchanged: true, want: "ai"},
		{name: "edited AI translation", existing: "ai", unchanged: false, want: "human_corrected"},
		{name: "unchanged imported text", existing: "import", unchanged: true, want: "import"},
		{name: "edited imported text", existing: "import", unchanged: false, want: "human"},
		{name: "unchanged human correction", existing: "human_corrected", unchanged: true, want: "human_corrected"},
		{name: "edited human correction", existing: "human_corrected", unchanged: false, want: "human_corrected"},
		{name: "new human text", existing: "", unchanged: false, want: "human"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := modContentResourceLocalizationProvenance(test.existing, test.unchanged); got != test.want {
				t.Fatalf("modContentResourceLocalizationProvenance(%q, %t) = %q, want %q", test.existing, test.unchanged, got, test.want)
			}
		})
	}
}

func TestModContentResourceCreateRejectsNonEditableLocale(t *testing.T) {
	t.Parallel()
	edit := modContentResourceEdit{
		ResourcePublicID: "abc123456",
		VersionPublicID:  "def123456",
		DefaultLocale:    "en-US",
		Localizations: []catalogLocalizationEdit{
			{Locale: "en-US", Name: "Copper"},
			{Locale: "pt-BR", Name: "Cobre"},
		},
	}
	if err := normalizeModContentResourceEdit(&edit); err == nil {
		t.Fatal("resource creation accepted a non-editable locale")
	}
}

func TestModContentResourceUpdatePreservesNonEditableLocales(t *testing.T) {
	t.Parallel()
	existing := map[string]modContentResourceLocalizationState{
		"pt-BR": {
			Name:            "Cobre",
			Summary:         "Resumo importado",
			ContentMarkdown: "Conteúdo importado",
			Provenance:      "import",
		},
	}
	edit := modContentResourceEdit{
		ResourcePublicID: "abc123456",
		VersionPublicID:  "def123456",
		DefaultLocale:    "en-US",
		Definition:       map[string]any{"hardness": 4},
		Localizations: []catalogLocalizationEdit{
			{Locale: "en-US", Name: "Edited copper", Summary: "Edited summary"},
			{Locale: "pt-BR", Name: "Cobre", Summary: "Resumo importado", ContentMarkdown: "Conteúdo importado"},
		},
	}
	if err := normalizeModContentResourceUpdateEdit(&edit); err != nil {
		t.Fatalf("normalizeModContentResourceUpdateEdit() error = %v", err)
	}
	if err := preserveImmutableModContentResourceLocales("en-US", existing, &edit); err != nil {
		t.Fatalf("unchanged non-editable locale was rejected: %v", err)
	}
	if edit.Localizations[1].Provenance != "import" || edit.Localizations[1].Editable == nil || *edit.Localizations[1].Editable {
		t.Fatalf("non-editable locale metadata was not preserved: %#v", edit.Localizations[1])
	}
	if got := edit.Definition["hardness"]; got != 4 {
		t.Fatalf("structured fields were unexpectedly changed: %#v", edit.Definition)
	}
}

func TestModContentResourceUpdateRejectsNonEditableLocaleMutation(t *testing.T) {
	t.Parallel()
	existing := map[string]modContentResourceLocalizationState{
		"pt-BR": {
			Name:            "Cobre",
			Summary:         "Resumo importado",
			ContentMarkdown: "Conteúdo importado",
			Provenance:      "import",
		},
	}
	tests := []struct {
		name            string
		existingDefault string
		edit            modContentResourceEdit
	}{
		{
			name:            "change localized text",
			existingDefault: "en-US",
			edit: modContentResourceEdit{
				DefaultLocale: "en-US",
				Localizations: []catalogLocalizationEdit{
					{Locale: "en-US", Name: "Copper"},
					{Locale: "pt-BR", Name: "Cobre alterado", Summary: "Resumo importado", ContentMarkdown: "Conteúdo importado"},
				},
			},
		},
		{
			name:            "remove locale",
			existingDefault: "en-US",
			edit: modContentResourceEdit{
				DefaultLocale: "en-US",
				Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Copper"}},
			},
		},
		{
			name:            "add locale",
			existingDefault: "en-US",
			edit: modContentResourceEdit{
				DefaultLocale: "en-US",
				Localizations: []catalogLocalizationEdit{
					{Locale: "en-US", Name: "Copper"},
					{Locale: "pt-BR", Name: "Cobre", Summary: "Resumo importado", ContentMarkdown: "Conteúdo importado"},
					{Locale: "ko-KR", Name: "구리"},
				},
			},
		},
		{
			name:            "change non-editable default",
			existingDefault: "pt-BR",
			edit: modContentResourceEdit{
				DefaultLocale: "en-US",
				Localizations: []catalogLocalizationEdit{
					{Locale: "en-US", Name: "Copper"},
					{Locale: "pt-BR", Name: "Cobre", Summary: "Resumo importado", ContentMarkdown: "Conteúdo importado"},
				},
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			test.edit.ResourcePublicID = "abc123456"
			test.edit.VersionPublicID = "def123456"
			if err := normalizeModContentResourceUpdateEdit(&test.edit); err != nil {
				t.Fatalf("update normalization failed before immutable comparison: %v", err)
			}
			if err := preserveImmutableModContentResourceLocales(test.existingDefault, existing, &test.edit); err == nil {
				t.Fatal("non-editable locale mutation was accepted")
			}
		})
	}
}
