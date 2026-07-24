package httpapi

import (
	"encoding/json"
	"testing"
)

func TestImportedCatalogLocalizationRowsNormalizeMinecraftLocales(t *testing.T) {
	rows := importedCatalogLocalizationRows([]byte(`{"en_us":"Copper Block","zh_cn":"铜块","zh_tw":"銅塊","pt_br":"Bloco de cobre"}`))
	got := map[string]string{}
	for _, row := range rows {
		locale, _ := row["locale"].(string)
		name, _ := row["name"].(string)
		got[locale] = name
		if row["provenance"] != "import" || row["editable"] != true {
			t.Fatalf("imported row did not retain provenance/editability: %#v", row)
		}
	}
	want := map[string]string{"en-US": "Copper Block", "zh-CN": "铜块", "zh-TW": "銅塊"}
	encodedGot, _ := json.Marshal(got)
	encodedWant, _ := json.Marshal(want)
	if string(encodedGot) != string(encodedWant) {
		t.Fatalf("normalized names = %s, want %s", encodedGot, encodedWant)
	}
}

func TestCatalogImportedEditorLocalizationsPreferImportedTitleNames(t *testing.T) {
	rows, defaultLocale := catalogImportedEditorLocalizations([]byte(`{"ja_jp":"圧縮","en_us":"Compressing"}`), "zh-CN", "mod:compressing")
	if defaultLocale != "en-US" || len(rows) != 2 {
		t.Fatalf("unexpected import localization fallback: default=%q rows=%#v", defaultLocale, rows)
	}
	if !catalogLocalizationMapContains(rows, "en-US") || !catalogLocalizationMapContains(rows, "ja-JP") {
		t.Fatalf("title_names were not normalized into editable site locales: %#v", rows)
	}
}

func TestCatalogImportedEditorLocalizationsGenerateStableDefaultName(t *testing.T) {
	rows, defaultLocale := catalogImportedEditorLocalizations(nil, "zh_tw", "  mod:stable_recipe_id  ")
	if defaultLocale != "zh-TW" || len(rows) != 1 {
		t.Fatalf("unexpected stable fallback: default=%q rows=%#v", defaultLocale, rows)
	}
	if rows[0]["locale"] != "zh-TW" || rows[0]["name"] != "mod:stable_recipe_id" || rows[0]["provenance"] != "import" || rows[0]["editable"] != true {
		t.Fatalf("generated fallback is not editable imported content: %#v", rows[0])
	}
}

func TestCatalogLocalizationProvenanceUsesResolvedCanonicalLocale(t *testing.T) {
	raw := []byte(`{"zh-CN":"human","en":"ai"}`)
	if got := catalogLocalizationProvenance(raw, "zh_cn", "名称"); got != "human" {
		t.Fatalf("resolved canonical locale provenance = %q, want human", got)
	}
	if got := catalogLocalizationProvenance(raw, "ja", "Imported name"); got != "import" {
		t.Fatalf("import-only locale provenance = %q, want import", got)
	}
}

func TestContentLocalizationAggregateKeyIncludesSubjectType(t *testing.T) {
	modKey := contentLocalizationAggregateKey("abc234567", "mod", "zh_cn")
	blueprintKey := contentLocalizationAggregateKey("abc234567", "blueprint", "zh-CN")
	if modKey == blueprintKey || modKey != "mod:abc234567:zh-CN" {
		t.Fatalf("aggregate keys must be normalized and type-scoped: %q / %q", modKey, blueprintKey)
	}
}

func TestCatalogHumanEditProvenance(t *testing.T) {
	for existing, want := range map[string]string{
		"ai": "human_corrected", "human_corrected": "human_corrected", "human": "human", "import": "human",
	} {
		if got := catalogHumanEditProvenance(existing); got != want {
			t.Fatalf("catalogHumanEditProvenance(%q) = %q, want %q", existing, got, want)
		}
	}
}
