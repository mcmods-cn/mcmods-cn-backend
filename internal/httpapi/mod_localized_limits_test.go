package httpapi

import (
	"strings"
	"testing"
)

func TestModNameAndSummaryUseDisplayedCharacterLimits(t *testing.T) {
	name := strings.Repeat("𠮷", 160)
	summary := strings.Repeat("界", 500)
	base := createModRequest{PrimaryName: name, Summary: summary, Environment: "bothRequired", PrimaryCategory: "technology",
		OfficialStatus: "active", SourceStatus: "open", License: "MIT", DefaultLocale: "zh-CN"}
	for _, tc := range []struct {
		label     string
		name      string
		summary   string
		localized bool
		valid     bool
	}{
		{"fallback at the limit", name, summary, false, true},
		{"explicit at the limit", name, summary, true, true},
		{"primary name over limit", name + "甲", summary, false, false},
		{"localized name over limit", name + "甲", summary, true, false},
		{"summary over limit", name, summary + "一", true, false},
	} {
		t.Run(tc.label, func(t *testing.T) {
			request := base
			if tc.localized {
				request.Localizations = []catalogLocalizationEdit{
					{Locale: "zh-CN", Name: tc.name, Summary: tc.summary},
					{Locale: "en-US", Name: strings.Repeat("a", 512)},
				}
			} else {
				request.PrimaryName, request.Summary = tc.name, tc.summary
			}
			err := normalizeAndValidateModRequest(&request)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if tc.valid && (request.SecondaryName != tc.name || request.Summary != tc.summary) {
				t.Fatal("localized name or summary did not round-trip intact")
			}
		})
	}
}
