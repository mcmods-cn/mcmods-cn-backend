package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestFirstAcceptedContentLocaleHonorsQualityWeights(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "higher quality wins", header: "fr-FR;q=0.2, en-US;q=0.9", want: "en-US"},
		{name: "ties retain field order", header: "de-DE;q=0.8, es-ES;q=0.8", want: "de-DE"},
		{name: "zero quality is rejected", header: "fr-FR;q=0, ja-JP;q=0.4", want: "ja-JP"},
		{name: "wildcard cannot override explicit refusal", header: "fr-FR;q=0, *;q=1", want: ""},
		{name: "invalid quality is ignored", header: "fr-FR;q=bogus, ru-RU;q=0.5", want: "ru-RU"},
		{name: "out of range quality is ignored", header: "de-DE;q=1.001, es-ES;q=0.5", want: "es-ES"},
		{name: "unknown parameter is ignored", header: "fr-FR;level=1, en-US;q=0.5", want: "en-US"},
		{name: "default quality is one", header: "ja-JP;q=0.9, zh-Hant", want: "zh-TW"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := firstAcceptedContentLocale(test.header); got != test.want {
				t.Fatalf("firstAcceptedContentLocale(%q) = %q, want %q", test.header, got, test.want)
			}
		})
	}
}

func TestPublicContentLocaleCallersShareWeightedNegotiation(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/content/abc234567", nil)
	request.Header.Set("Accept-Language", "fr-FR;q=0, en-US;q=0.8")
	primary, secondary := (&Server{}).requestContentLocales(request)
	if primary != "en-US" || secondary != "en-US" {
		t.Fatalf("content localization preferences = (%q, %q), want (en-US, en-US)", primary, secondary)
	}

	catalogRequest := httptest.NewRequest(http.MethodGet, "/api/v1/modpacks", nil)
	catalogRequest.Header.Set("Accept-Language", "de-DE;q=0.2, ja-JP;q=0.9")
	locale, ok := simpleProjectCatalogLocale(catalogRequest)
	if !ok || locale != "ja-JP" {
		t.Fatalf("simple project locale = (%q, %t), want (ja-JP, true)", locale, ok)
	}
}

func TestResolveContentLocaleChinesePairBeforeEnglish(t *testing.T) {
	resolution := resolveContentLocale("zh-TW", "en", "en-US", []string{"en_us", "zh-CN"})
	if resolution.ResolvedLocale != "zh-CN" || resolution.Reason != "chinese_pair" {
		t.Fatalf("unexpected resolution: %#v", resolution)
	}
	if !resolution.CanRequestTranslation {
		t.Fatalf("supported missing locale must require an explicit quota-backed translation: %#v", resolution)
	}
}

func TestResolveContentLocaleUnsupportedUsesSecondary(t *testing.T) {
	resolution := resolveContentLocale("pt-BR", "ja", "en-US", []string{"en-US", "ja_jp"})
	if resolution.ResolvedLocale != "ja-JP" || resolution.Reason != "secondary" {
		t.Fatalf("unexpected resolution: %#v", resolution)
	}
	if !resolution.CanRequestTranslation || resolution.RequestedEditable {
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
	first := contentTranslationConcurrencyKey("mod", 1, source, "ja", 11)
	updated := contentTranslationConcurrencyKey("mod", 1, catalogLocalizationPayload{Locale: "zh-CN", RevisionNo: 4}, "ja", 11)
	if first == contentTranslationConcurrencyKey("blueprint", 1, source, "ja", 11) {
		t.Fatal("independent subject sequences must not reuse another resource type's task")
	}
	if first == updated {
		t.Fatal("a source revision change must create a new translation task key")
	}
	firstUser := contentTranslationConcurrencyKey("mod", 1, source, "pt-BR", 11)
	secondUser := contentTranslationConcurrencyKey("mod", 1, source, "pt-BR", 12)
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
