package httpapi

import "testing"

func TestNormalizeSkinAssetUpdateCarriesOneLocalizationSnapshot(t *testing.T) {
	request := skinAssetUpdateRequest{
		DefaultLocale: "zh_cn",
		Localizations: []catalogLocalizationEdit{
			{Locale: "en_us", Name: "English", Summary: "Summary"},
			{Locale: "zh_cn", Name: " 中文名 ", Summary: " 中文介绍 "},
		},
	}
	replace, err := normalizeSkinAssetUpdateRequest(&request)
	if err != nil {
		t.Fatal(err)
	}
	if !replace || request.DefaultLocale != "zh-CN" || request.Name == nil || *request.Name != "中文名" || request.Description == nil || *request.Description != "中文介绍" {
		t.Fatalf("skin localization snapshot was not normalized into base metadata: %#v", request)
	}
	if len(request.Localizations) != 2 || request.Localizations[0].Locale != "en-US" {
		t.Fatalf("unexpected normalized localizations: %#v", request.Localizations)
	}
}

func TestNormalizeSkinAssetUpdatePreservesLegacyMetadataOnlyRequests(t *testing.T) {
	name := "Metadata only"
	request := skinAssetUpdateRequest{Name: &name}
	replace, err := normalizeSkinAssetUpdateRequest(&request)
	if err != nil || replace {
		t.Fatalf("metadata-only update replace=%v err=%v", replace, err)
	}
}

func TestNormalizeSkinAssetUpdateRejectsMissingDefaultLocalization(t *testing.T) {
	request := skinAssetUpdateRequest{
		DefaultLocale: "zh-CN",
		Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "English"}},
	}
	if _, err := normalizeSkinAssetUpdateRequest(&request); err == nil {
		t.Fatal("expected a missing default localization error")
	}
}
