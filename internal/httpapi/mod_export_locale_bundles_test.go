package httpapi

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"strings"
	"testing"
)

func TestPrepareExportLocaleBundlesPreservesColdLocaleDictionary(t *testing.T) {
	translation := preparedExportTranslation{
		Locale: "ko-KR",
		Values: map[string]string{
			"item.minecraft.stone": "Stone in Korean",
			"block.minecraft.dirt": "Dirt in Korean",
		},
	}
	bundles, payload, err := prepareExportLocaleBundles(
		ossConfigPayload{Prefix: "mcmods"},
		"prj_test",
		map[string]string{"minecraft": "revision-test"},
		translation,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 1 || bundles[0].Locale != "ko-KR" ||
		bundles[0].AssetPath != "_locales/ko-KR.json.gz" ||
		bundles[0].ContentType != exportLocaleBundleContentType {
		t.Fatalf("unexpected locale bundle metadata: %#v", bundles)
	}
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var decoded map[string]string
	if err = json.NewDecoder(reader).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["item.minecraft.stone"] != "Stone in Korean" || len(decoded) != len(translation.Values) {
		t.Fatalf("cold locale dictionary was not preserved: %#v", decoded)
	}
}

func TestPrepareExportLocaleBundlesKeepsRegionalLocalesDistinct(t *testing.T) {
	revisions := map[string]string{
		"minecraft": "revision-test",
		"alias":     "revision-test",
	}
	taiwanBundles, _, err := prepareExportLocaleBundles(
		ossConfigPayload{Prefix: "mcmods"},
		"prj_test",
		revisions,
		preparedExportTranslation{
			Locale: "zh-TW",
			Values: map[string]string{"item.minecraft.stone": "石頭"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	hongKongBundles, _, err := prepareExportLocaleBundles(
		ossConfigPayload{Prefix: "mcmods"},
		"prj_test",
		revisions,
		preparedExportTranslation{
			Locale: "zh-HK",
			Values: map[string]string{"item.minecraft.stone": "石頭"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(taiwanBundles) != 1 || len(hongKongBundles) != 1 {
		t.Fatalf("duplicate revision aliases produced duplicate bundles: Taiwan=%d Hong Kong=%d", len(taiwanBundles), len(hongKongBundles))
	}
	if taiwanBundles[0].Locale == hongKongBundles[0].Locale ||
		taiwanBundles[0].AssetPath == hongKongBundles[0].AssetPath ||
		taiwanBundles[0].ObjectKey == hongKongBundles[0].ObjectKey {
		t.Fatalf("regional bundles collided: Taiwan=%#v Hong Kong=%#v", taiwanBundles[0], hongKongBundles[0])
	}
	if taiwanBundles[0].AssetPath != "_locales/zh-TW.json.gz" ||
		hongKongBundles[0].AssetPath != "_locales/zh-HK.json.gz" {
		t.Fatalf("unexpected regional asset paths: Taiwan=%s Hong Kong=%s", taiwanBundles[0].AssetPath, hongKongBundles[0].AssetPath)
	}
}

func TestExportLocaleBundleByteBudgetMatchesStoredJSON(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"nil map":                         nil,
		"empty map":                       {},
		"empty value and key":             {"": ""},
		"reserved and control characters": {"a<&>\u2029": "\u0000\u001f\b\f\n\r\t\"\\&<>\u2028"},
		"valid and invalid UTF-8":         {"normal": "世界🌍�", "invalid": "\xff\xe0"},
		"split rune":                      {"source": strings.Repeat("a", (16<<10)-1) + "🌲雪<&>"},
	} {
		t.Run(name, func(t *testing.T) {
			var stored bytes.Buffer
			if err := json.NewEncoder(&stored).Encode(values); err != nil {
				t.Fatal(err)
			}
			size := int64(stored.Len())
			if !exportLocaleBundleFits(values, size) || exportLocaleBundleFits(values, size-1) {
				t.Fatalf("size check disagrees with the %d-byte stored JSON", size)
			}
		})
	}
}
