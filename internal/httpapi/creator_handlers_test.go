package httpapi

import "testing"

func TestNormalizeCreatorSnapshotCreatesDefaultLocalization(t *testing.T) {
	snapshot := creatorSnapshot{Kind: "author", Name: "测试作者", DescriptionMarkdown: "中文介绍"}
	if err := normalizeCreatorSnapshot(&snapshot); err != nil {
		t.Fatalf("normalizeCreatorSnapshot returned error: %v", err)
	}
	if snapshot.DefaultLocale != "zh-CN" {
		t.Fatalf("default locale = %q, want zh-CN", snapshot.DefaultLocale)
	}
	if len(snapshot.Localizations) != 1 || snapshot.Localizations[0].ContentMarkdown != "中文介绍" {
		t.Fatalf("unexpected localizations: %#v", snapshot.Localizations)
	}
}

func TestNormalizeCreatorSnapshotKeepsCanonicalName(t *testing.T) {
	snapshot := creatorSnapshot{
		Kind:                "author",
		Name:                "Canonical author name",
		DescriptionMarkdown: "Legacy introduction",
		DefaultLocale:       "en-US",
		Localizations: []creatorLocalizationEdit{
			{Locale: "zh-CN", ContentMarkdown: "中文介绍"},
			{Locale: "en-US", ContentMarkdown: "English introduction"},
		},
	}
	if err := normalizeCreatorSnapshot(&snapshot); err != nil {
		t.Fatalf("normalizeCreatorSnapshot returned error: %v", err)
	}
	if snapshot.Name != "Canonical author name" || snapshot.DescriptionMarkdown != "English introduction" {
		t.Fatalf("creator name changed or default introduction was not promoted: %#v", snapshot)
	}
}
