package httpapi

import (
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestNotificationTemplateUsesConfiguredChineseTranslation(t *testing.T) {
	config := mergeNotificationTemplates(notificationTemplateConfig{Templates: []notificationTemplateDefinition{{
		Code: "project_updated", Version: 7, Variables: []string{"project_name", "changed_sections"},
		Translations: map[string]localizedNotificationTemplate{
			"zh-CN": {Title: "后台中文标题", Body: "{project_name} 更新了{changed_sections}"},
			"en-US": {Title: "Configured English", Body: "{project_name} updated {changed_sections}"},
		},
	}}})
	rendered, err := renderNotificationTemplateForLocale(config, "zh_cn", "project_updated", map[string]string{
		"project_name": "测试模组", "changed_sections": "详情介绍",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Locale != "zh-CN" || rendered.Title != "后台中文标题" || rendered.Body != "测试模组 更新了详情介绍" || rendered.Version != 7 {
		t.Fatalf("unexpected rendered template: %#v", rendered)
	}
}

func TestDefaultNotificationTemplatesCoverEveryEnabledLocale(t *testing.T) {
	locales := editableStickerLocales()
	for _, template := range defaultNotificationTemplateConfig().Templates {
		for _, locale := range locales {
			value, ok := template.Translations[locale]
			if !ok || value.Title == "" || value.Body == "" {
				t.Fatalf("template %q is missing locale %q", template.Code, locale)
			}
		}
	}
}

func TestNotificationTemplateRejectsMissingVariables(t *testing.T) {
	_, err := renderNotificationTemplateForLocale(defaultNotificationTemplateConfig(), "en-US", "project_updated", map[string]string{"project_name": "Example"})
	if err == nil {
		t.Fatal("expected a missing-template-variable error")
	}
}

func TestNotificationTemplateVersionChangesOnlyWithContent(t *testing.T) {
	current := notificationTemplateConfig{Templates: []notificationTemplateDefinition{{
		Code: "example", Version: 4, Variables: []string{"name"},
		Translations: map[string]localizedNotificationTemplate{"zh-CN": {Title: "标题", Body: "{name}"}},
	}}}
	unchanged := versionNotificationTemplateChanges(current, current)
	if unchanged.Templates[0].Version != 4 {
		t.Fatalf("unchanged template version = %d, want 4", unchanged.Templates[0].Version)
	}
	changed := notificationTemplateConfig{Templates: []notificationTemplateDefinition{{
		Code: "example", Version: 99, Variables: []string{"name"},
		Translations: map[string]localizedNotificationTemplate{"zh-CN": {Title: "新标题", Body: "{name}"}},
	}}}
	changed = versionNotificationTemplateChanges(current, changed)
	if changed.Templates[0].Version != 5 {
		t.Fatalf("changed template version = %d, want 5", changed.Templates[0].Version)
	}
}

func TestStickerCodeAndAllLocaleNames(t *testing.T) {
	for _, value := range []string{"dogs", "dogs_2", "dogs-happy"} {
		if !validStickerCode(value) {
			t.Fatalf("expected valid sticker code %q", value)
		}
	}
	for _, value := range []string{"Dogs", "dog:happy", "dog happy", "_dogs", "狗"} {
		if validStickerCode(value) {
			t.Fatalf("expected invalid sticker code %q", value)
		}
	}
	names := stickerTranslationMap{}
	for _, locale := range editableStickerLocales() {
		names[locale] = locale + " name"
	}
	result, err := normalizeStickerTranslations(names)
	if err != nil || len(result) != len(editableStickerLocales()) {
		t.Fatalf("valid multilingual names were rejected: result=%#v err=%v", result, err)
	}
	delete(names, editableStickerLocales()[0])
	if _, err = normalizeStickerTranslations(names); err == nil {
		t.Fatal("expected incomplete multilingual sticker names to be rejected")
	}
}

func TestFeatureLimitDefaultsAndOverrides(t *testing.T) {
	defaults := normalizedStickerLimits(config.StickerConfig{})
	if defaults.MaxBytes != 4<<20 || defaults.MaxEdge != 1024 || defaults.MaxGIFDuration != 30*time.Second {
		t.Fatalf("unexpected sticker defaults: %#v", defaults)
	}
	override := normalizedStickerLimits(config.StickerConfig{MaxBytes: 1024, MaxEdge: 64, MaxPixels: 4096, MaxGIFFrames: 2, MaxGIFDecodedPixels: 8192, MaxGIFDuration: time.Second})
	if override.MaxBytes != 1024 || override.MaxGIFFrames != 2 || override.MaxGIFDuration != time.Second {
		t.Fatalf("sticker overrides were not preserved: %#v", override)
	}
	if favoriteExportMaxActive(0) != 2 || favoriteExportMaxActive(5) != 5 || favoriteExportDailyLimit(0) != 20 || favoriteExportDailyLimit(30) != 30 || favoriteExportMaxAttempts(0) != 3 || favoriteExportMaxAttempts(7) != 7 {
		t.Fatal("favorite export defaults or overrides are incorrect")
	}
}

func TestProjectUpdateSectionsAreStableAndIgnoreAdministrativeFields(t *testing.T) {
	got := uniqueProjectUpdateSections([]string{"/description/zh-CN", "description/en-US", "reason", "downloads/files", "downloads/files"})
	want := []string{"description", "downloads"}
	if len(got) != len(want) {
		t.Fatalf("sections=%#v want=%#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("sections=%#v want=%#v", got, want)
		}
	}
}
