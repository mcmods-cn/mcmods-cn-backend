package httpapi

import "strings"

var exportContentLocaleByCode = map[string]string{
	"zh_cn": "zh-CN",
	"zh_tw": "zh-TW",
	"en_us": "en-US",
	"ja_jp": "ja-JP",
	"ru_ru": "ru-RU",
	"fr_fr": "fr-FR",
	"de_de": "de-DE",
	"es_es": "es-ES",
}

func exportContentLocale(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if locale, supported := exportContentLocaleByCode[strings.ToLower(value)]; supported {
		return locale, true
	}
	for _, locale := range exportContentLocaleByCode {
		if strings.EqualFold(value, locale) {
			return locale, true
		}
	}
	return "", false
}

// canonicalExportLocaleTag converts Minecraft language file codes to BCP-47
// casing without applying semantic aliases. In particular, zh-TW, zh-HK, and
// zh-MO must remain distinct cold locale bundles.
func canonicalExportLocaleTag(value string) (string, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "_", "-"))
	if !validContentLocaleTag(value) {
		return "", false
	}
	parts := strings.Split(value, "-")
	for index, part := range parts {
		part = strings.ToLower(part)
		switch {
		case index == 0:
			parts[index] = part
		case len(part) == 2 || len(part) == 3 && isASCIIDigits(part):
			parts[index] = strings.ToUpper(part)
		case len(part) == 4 && isASCIIAlpha(part):
			parts[index] = strings.ToUpper(part[:1]) + part[1:]
		default:
			parts[index] = part
		}
	}
	return strings.Join(parts, "-"), true
}

func supportedExportNames(value any) map[string]any {
	raw, _ := value.(map[string]any)
	result := make(map[string]any, len(exportContentLocaleByCode))
	for sourceLocale, localized := range raw {
		locale, supported := exportContentLocale(sourceLocale)
		if !supported {
			continue
		}
		result[locale] = localized
	}
	return result
}

// filterSupportedExportLocalizedFields removes locale variants which the site
// cannot edit or display. Exporter documents may repeat the same 100+ locale
// map in names, tooltips, and nested description fields.
func filterSupportedExportLocalizedFields(value any) {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			normalizedKey := strings.ToLower(strings.TrimSpace(key))
			if normalizedKey == "names" || normalizedKey == "tooltips" || strings.HasSuffix(normalizedKey, "_names") {
				current[key] = supportedExportNames(child)
				continue
			}
			filterSupportedExportLocalizedFields(child)
		}
	case []any:
		for _, child := range current {
			filterSupportedExportLocalizedFields(child)
		}
	}
}
