package httpapi

import (
	"testing"
	"time"
)

func TestNormalizeProjectChangelogSnapshot(t *testing.T) {
	t.Parallel()
	snapshot := projectChangelogSnapshot{
		EventAt:           time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC),
		MinecraftVersions: []string{"1.21.1", " 1.21.1 ", "1.20.1"},
		ProjectVersion:    " 10.8.0 ",
		DefaultLocale:     "zh_cn",
		Localizations: []projectChangelogLocalization{
			{Locale: "zh-CN", BodyMarkdown: "  修复问题  "},
			{Locale: "en", BodyMarkdown: "Fixed issues"},
		},
	}
	if err := normalizeProjectChangelogSnapshot(&snapshot); err != nil {
		t.Fatalf("normalize valid changelog: %v", err)
	}
	if snapshot.ProjectVersion != "10.8.0" || snapshot.DefaultLocale != "zh-CN" {
		t.Fatalf("changelog scalar fields were not normalized: %#v", snapshot)
	}
	if len(snapshot.MinecraftVersions) != 2 || snapshot.Localizations[1].Locale != "en-US" {
		t.Fatalf("changelog lists were not normalized: %#v", snapshot)
	}
}

func TestNormalizeProjectChangelogSnapshotRequiresDefaultBody(t *testing.T) {
	t.Parallel()
	snapshot := projectChangelogSnapshot{
		EventAt: time.Now(), MinecraftVersions: []string{"1.21.1"}, ProjectVersion: "1.0.0", DefaultLocale: "zh-CN",
		Localizations: []projectChangelogLocalization{{Locale: "en-US", BodyMarkdown: "Only English"}},
	}
	if err := normalizeProjectChangelogSnapshot(&snapshot); err == nil {
		t.Fatal("missing default-language body was accepted")
	}
}

func TestNormalizeProjectChangelogSnapshotRejectsTwoCategorySources(t *testing.T) {
	t.Parallel()
	snapshot := projectChangelogSnapshot{
		EventAt: time.Now(), MinecraftVersions: []string{"1.21.1"}, ProjectVersion: "1.0.0", DefaultLocale: "zh-CN",
		CategoryID:    "abc234567",
		NewCategory:   &projectChangelogNewCategory{DefaultLocale: "zh-CN", Localizations: []projectChangelogCategoryLocalization{{Locale: "zh-CN", Name: "稳定版"}}},
		Localizations: []projectChangelogLocalization{{Locale: "zh-CN", BodyMarkdown: "内容"}},
	}
	if err := normalizeProjectChangelogSnapshot(&snapshot); err == nil {
		t.Fatal("existing and new changelog categories were accepted together")
	}
}

func TestNormalizeChangelogTargetType(t *testing.T) {
	t.Parallel()
	if got := normalizeChangelogTargetType("server"); got != "minecraft_server" {
		t.Fatalf("server alias = %q", got)
	}
	if got := normalizeChangelogTargetType("resource-pack"); got != "resource_pack" {
		t.Fatalf("resource pack alias = %q", got)
	}
	if got := normalizeChangelogTargetType("user"); got != "" {
		t.Fatalf("unsupported target = %q", got)
	}
}
