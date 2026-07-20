package httpapi

import (
	"net/http"
	"testing"
)

func TestResolveContentLocaleChinesePairBeforeEnglish(t *testing.T) {
	resolution := resolveContentLocale("zh-TW", "en", "en", []string{"en", "zh-CN"})
	if resolution.ResolvedLocale != "zh-CN" || resolution.Reason != "chinese_pair" {
		t.Fatalf("unexpected resolution: %#v", resolution)
	}
	if !resolution.ShouldAutoTranslate || resolution.CanRequestTranslation {
		t.Fatalf("supported missing locale must be an automatic free translation: %#v", resolution)
	}
}

func TestResolveContentLocaleUnsupportedUsesSecondary(t *testing.T) {
	resolution := resolveContentLocale("pt-BR", "ja", "en", []string{"en", "ja"})
	if resolution.ResolvedLocale != "ja" || resolution.Reason != "secondary" {
		t.Fatalf("unexpected resolution: %#v", resolution)
	}
	if resolution.ShouldAutoTranslate || !resolution.CanRequestTranslation || resolution.RequestedEditable {
		t.Fatalf("unsupported locale must require an explicit quota-backed translation: %#v", resolution)
	}
}

func TestResolveContentLocaleStoredUnsupportedTranslation(t *testing.T) {
	resolution := resolveContentLocale("pt_br", "en", "en", []string{"pt-BR", "en"})
	if resolution.ResolvedLocale != "pt-BR" || resolution.Reason != "exact" || !resolution.RequestedExists {
		t.Fatalf("unexpected resolution: %#v", resolution)
	}
	if resolution.RequestedEditable {
		t.Fatal("stored unsupported translation must remain non-editable")
	}
}

func TestNormalizeContentLocaleChineseAliases(t *testing.T) {
	tests := map[string]string{
		"zh_hans": "zh-CN",
		"zh-HK":   "zh-TW",
		"PT_br":   "pt-BR",
	}
	for input, want := range tests {
		if got := normalizeContentLocale(input); got != want {
			t.Fatalf("normalizeContentLocale(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidContentLocaleTag(t *testing.T) {
	for _, locale := range []string{"zh-CN", "pt-BR", "sr-Latn-RS", "en"} {
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

func TestCatalogReviewAndBypassPermissionsAreSeparated(t *testing.T) {
	if catalogMutationBypassesReview([]string{"content.review"}) {
		t.Fatal("content.review must not bypass the configured review workflow")
	}
	if !catalogMutationBypassesReview([]string{"content.no-review"}) {
		t.Fatal("content.no-review must bypass the configured review workflow")
	}
	if !catalogMutationBypassesReview([]string{"admin.*"}) {
		t.Fatal("administrators must retain the explicit review bypass")
	}
}

func TestContentTranslationConcurrencyKeyTracksSourceAndActor(t *testing.T) {
	source := catalogLocalizationPayload{Locale: "zh_cn", RevisionNo: 3}
	free := contentTranslationConcurrencyKey("entity", source, "ja", 0, false)
	updated := contentTranslationConcurrencyKey("entity", catalogLocalizationPayload{Locale: "zh-CN", RevisionNo: 4}, "ja", 0, false)
	if free == updated {
		t.Fatal("a source revision change must create a new translation task key")
	}
	firstUser := contentTranslationConcurrencyKey("entity", source, "pt-BR", 11, true)
	secondUser := contentTranslationConcurrencyKey("entity", source, "pt-BR", 12, true)
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

func TestUnifiedContentWriteRoutesAreRegistered(t *testing.T) {
	server := &Server{mux: http.NewServeMux()}
	server.routes()
	for _, path := range []string{"/api/v1/content/abc234567", "/api/v1/catalog/entities/abc234567/content"} {
		request, err := http.NewRequest(http.MethodPut, path, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, pattern := server.mux.Handler(request)
		if pattern == "" {
			t.Fatalf("unified content write route is not registered: %s", path)
		}
	}
}
