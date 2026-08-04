package httpapi

import (
	"net/http"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestResolveContentLocaleChinesePairBeforeEnglish(t *testing.T) {
	resolution := resolveContentLocale("zh-TW", "en", "en-US", []string{"en_us", "zh-CN"})
	if resolution.ResolvedLocale != "zh-CN" || resolution.Reason != "chinese_pair" {
		t.Fatalf("unexpected resolution: %#v", resolution)
	}
	if !resolution.ShouldAutoTranslate || resolution.CanRequestTranslation {
		t.Fatalf("supported missing locale must be an automatic free translation: %#v", resolution)
	}
}

func TestResolveContentLocaleUnsupportedUsesSecondary(t *testing.T) {
	resolution := resolveContentLocale("pt-BR", "ja", "en-US", []string{"en-US", "ja_jp"})
	if resolution.ResolvedLocale != "ja-JP" || resolution.Reason != "secondary" {
		t.Fatalf("unexpected resolution: %#v", resolution)
	}
	if resolution.ShouldAutoTranslate || !resolution.CanRequestTranslation || resolution.RequestedEditable {
		t.Fatalf("unsupported locale must require an explicit quota-backed translation: %#v", resolution)
	}
}

func TestResolveContentLocaleStoredUnsupportedTranslation(t *testing.T) {
	resolution := resolveContentLocale("pt_br", "en", "en-US", []string{"pt-BR", "en-US"})
	if resolution.ResolvedLocale != "pt-BR" || resolution.Reason != "exact" || !resolution.RequestedExists {
		t.Fatalf("unexpected resolution: %#v", resolution)
	}
	if resolution.RequestedEditable {
		t.Fatal("stored unsupported translation must remain non-editable")
	}
}

func TestNormalizeContentLocaleChineseAliases(t *testing.T) {
	tests := map[string]string{
		"zh_hans":    "zh-CN",
		"zh-HK":      "zh-HK",
		"zh-Hant-HK": "zh-HK",
		"zh-MO":      "zh-MO",
		"PT_br":      "pt-BR",
		"be_latn":    "be-Latn",
		"zlm_arab":   "zlm-Arab",
		"en":         "en-US",
		"ja_jp":      "ja-JP",
	}
	for input, want := range tests {
		if got := normalizeContentLocale(input); got != want {
			t.Fatalf("normalizeContentLocale(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidContentLocaleTag(t *testing.T) {
	for _, locale := range []string{"zh-CN", "pt-BR", "sr-Latn-RS", "en-US"} {
		if !validContentLocaleTag(locale) {
			t.Fatalf("expected %q to be accepted", locale)
		}
	}
	for _, locale := range []string{"", "x", "zh--CN", "not a locale", "en-012345678"} {
		if validContentLocaleTag(locale) {
			t.Fatalf("expected %q to be rejected", locale)
		}
	}
}

func TestSecondaryContentLocaleMustUseSiteSupportedLanguage(t *testing.T) {
	for _, locale := range supportedContentLocaleList() {
		if !isEditableContentLocale(locale) {
			t.Fatalf("site-supported locale %q must be valid as a secondary language", locale)
		}
	}
	if isEditableContentLocale("pt-BR") {
		t.Fatal("a Minecraft-only locale must not be valid as a secondary language")
	}
}

func TestCatalogReviewAndBypassPermissionsAreSeparated(t *testing.T) {
	claims := func(code string) security.Claims {
		return security.Claims{PermissionRules: []security.PermissionRule{{Code: code, Allow: true}}}
	}
	if catalogMutationBypassesReview(claims("content.review")) {
		t.Fatal("content.review must not bypass the configured review workflow")
	}
	if !catalogMutationBypassesReview(claims("content.no-review")) {
		t.Fatal("content.no-review must bypass the configured review workflow")
	}
	if !catalogMutationBypassesReview(claims("admin.*")) {
		t.Fatal("administrators must retain the explicit review bypass")
	}
}

func TestContentTranslationConcurrencyKeyTracksSourceAndActor(t *testing.T) {
	source := catalogLocalizationPayload{Locale: "zh_cn", RevisionNo: 3}
	free := contentTranslationConcurrencyKey(1, source, "ja", 0, false)
	updated := contentTranslationConcurrencyKey(1, catalogLocalizationPayload{Locale: "zh-CN", RevisionNo: 4}, "ja", 0, false)
	if free == updated {
		t.Fatal("a source revision change must create a new translation task key")
	}
	firstUser := contentTranslationConcurrencyKey(1, source, "pt-BR", 11, true)
	secondUser := contentTranslationConcurrencyKey(1, source, "pt-BR", 12, true)
	if firstUser == secondUser {
		t.Fatal("quota-backed tasks must be scoped to the requesting user")
	}
}

func TestContentLocalizationReviewPolicyUsesSubjectType(t *testing.T) {
	config := reviewConfig{CatalogEdit: true, ModEdit: false, BlueprintEdit: true}
	if contentLocalizationReviewRequired(config, "mod") {
		t.Fatal("mod localization policy must use modEdit")
	}
	if !contentLocalizationReviewRequired(config, "blueprint") || !contentLocalizationReviewRequired(config, "resource") {
		t.Fatal("blueprint/catalog localization policy was not selected")
	}
}

func TestContentWriteRouteIsRegistered(t *testing.T) {
	server := &Server{mux: http.NewServeMux()}
	server.routes()
	request, err := http.NewRequest(http.MethodPut, "/api/v1/content/abc234567", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, pattern := server.mux.Handler(request)
	if pattern == "" {
		t.Fatal("content write route is not registered")
	}
}
