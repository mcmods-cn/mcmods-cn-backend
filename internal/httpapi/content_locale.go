package httpapi

import (
	"sort"
	"strings"
)

// supportedEditableContentLocales is deliberately separate from UI locales.
// These are the locales for which mcmods.cn provides a human editing surface.
// Other valid BCP-47 locales can be persisted as non-editable AI translations.
var supportedEditableContentLocales = map[string]struct{}{
	"zh-CN": {},
	"zh-TW": {},
	"en-US": {},
	"ja-JP": {},
	"ru-RU": {},
	"fr-FR": {},
	"de-DE": {},
	"es-ES": {},
}

type contentLocaleResolution struct {
	RequestedLocale       string
	ResolvedLocale        string
	Reason                string
	RequestedExists       bool
	RequestedEditable     bool
	ShouldAutoTranslate   bool
	CanRequestTranslation bool
}

func normalizeContentLocale(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "_", "-"))
	if value == "" {
		return ""
	}
	parts := strings.Split(value, "-")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
		if parts[index] == "" {
			return ""
		}
		if index == 0 {
			parts[index] = strings.ToLower(parts[index])
			continue
		}
		if len(parts[index]) == 2 || len(parts[index]) == 3 && isASCIIDigits(parts[index]) {
			parts[index] = strings.ToUpper(parts[index])
		} else if len(parts[index]) == 4 && isASCIIAlpha(parts[index]) {
			parts[index] = strings.ToUpper(parts[index][:1]) + strings.ToLower(parts[index][1:])
		} else {
			parts[index] = strings.ToLower(parts[index])
		}
	}
	locale := strings.Join(parts, "-")
	switch strings.ToLower(locale) {
	case "zh", "zh-cn", "zh-sg", "zh-hans", "zh-hans-cn", "zh-hans-sg":
		return "zh-CN"
	case "zh-tw", "zh-hant", "zh-hant-tw":
		return "zh-TW"
	case "zh-hk", "zh-hant-hk":
		return "zh-HK"
	case "zh-mo", "zh-hant-mo":
		return "zh-MO"
	case "en":
		return "en-US"
	case "ja":
		return "ja-JP"
	case "ru":
		return "ru-RU"
	case "fr":
		return "fr-FR"
	case "de":
		return "de-DE"
	case "es":
		return "es-ES"
	}
	return locale
}

func isASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func isEditableContentLocale(locale string) bool {
	_, ok := supportedEditableContentLocales[normalizeContentLocale(locale)]
	return ok
}

func validContentLocaleTag(value string) bool {
	value = strings.TrimSpace(strings.ReplaceAll(value, "_", "-"))
	if value == "" || len(value) > 63 {
		return false
	}
	parts := strings.Split(value, "-")
	if len(parts[0]) < 2 || len(parts[0]) > 8 || !isASCIIAlpha(parts[0]) {
		return false
	}
	for _, part := range parts[1:] {
		if len(part) < 1 || len(part) > 8 || !isASCIIAlphanumeric(part) {
			return false
		}
	}
	return true
}

func isASCIIAlpha(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') {
			return false
		}
	}
	return true
}

func isASCIIAlphanumeric(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func supportedContentLocaleList() []string {
	result := make([]string, 0, len(supportedEditableContentLocales))
	for locale := range supportedEditableContentLocales {
		result = append(result, locale)
	}
	sort.Strings(result)
	return result
}

func resolveContentLocale(primary, secondary, defaultLocale string, available []string) contentLocaleResolution {
	primary = normalizeContentLocale(primary)
	secondary = normalizeContentLocale(secondary)
	defaultLocale = normalizeContentLocale(defaultLocale)
	if primary == "" {
		primary = "zh-CN"
	}
	if secondary == "" {
		secondary = "en-US"
	}
	if defaultLocale == "" {
		defaultLocale = "en-US"
	}

	availableSet := make(map[string]struct{}, len(available))
	availableOrder := make([]string, 0, len(available))
	for _, rawLocale := range available {
		locale := normalizeContentLocale(rawLocale)
		if locale == "" {
			continue
		}
		if _, exists := availableSet[locale]; exists {
			continue
		}
		availableSet[locale] = struct{}{}
		availableOrder = append(availableOrder, locale)
	}

	result := contentLocaleResolution{
		RequestedLocale:   primary,
		RequestedEditable: isEditableContentLocale(primary),
	}
	if _, ok := availableSet[primary]; ok {
		result.ResolvedLocale = primary
		result.Reason = "exact"
		result.RequestedExists = true
		return result
	}

	// A missing supported locale is generated automatically and does not use a
	// user's daily AI allowance. Unsupported locales are opt-in and consume it.
	result.ShouldAutoTranslate = result.RequestedEditable && len(availableSet) > 0
	result.CanRequestTranslation = !result.RequestedEditable && len(availableSet) > 0

	type candidate struct {
		locale string
		reason string
	}
	candidates := make([]candidate, 0, 8)
	appendCandidate := func(locale, reason string) {
		locale = normalizeContentLocale(locale)
		if locale == "" || locale == primary {
			return
		}
		for _, existing := range candidates {
			if existing.locale == locale {
				return
			}
		}
		candidates = append(candidates, candidate{locale: locale, reason: reason})
	}
	if primary == "zh-CN" {
		appendCandidate("zh-TW", "chinese_pair")
	} else if primary == "zh-TW" {
		appendCandidate("zh-CN", "chinese_pair")
	}
	appendCandidate(secondary, "secondary")
	if secondary == "zh-CN" {
		appendCandidate("zh-TW", "secondary_chinese_pair")
	} else if secondary == "zh-TW" {
		appendCandidate("zh-CN", "secondary_chinese_pair")
	}
	appendCandidate(defaultLocale, "default")
	appendCandidate("en-US", "english")
	for _, item := range candidates {
		if _, ok := availableSet[item.locale]; ok {
			result.ResolvedLocale = item.locale
			result.Reason = item.reason
			return result
		}
	}
	if len(availableOrder) > 0 {
		sort.Strings(availableOrder)
		result.ResolvedLocale = availableOrder[0]
		result.Reason = "first_available"
		return result
	}
	result.Reason = "missing"
	return result
}

func firstAcceptedContentLocale(header string) string {
	for _, rawItem := range strings.Split(header, ",") {
		item := strings.TrimSpace(strings.SplitN(rawItem, ";", 2)[0])
		if locale := normalizeContentLocale(item); locale != "" && locale != "*" {
			return locale
		}
	}
	return ""
}
