package httpapi

import (
	"strings"
	"testing"
)

func TestMarkdownDeltaCallersEnforceBusinessInputBudgets(t *testing.T) {
	if validCommentMarkdownSize(strings.Repeat("a", maxCommentMarkdownRunes+1)) {
		t.Fatal("comment larger than its business rune budget was accepted")
	}

	creator := creatorSnapshot{
		Kind:          "author",
		Name:          "bounded creator",
		DefaultLocale: "en-US",
		Localizations: []creatorLocalizationEdit{{
			Locale:          "en-US",
			ContentMarkdown: strings.Repeat("a", maxModExportEntryMarkdownBytes+1),
		}},
	}
	if err := normalizeCreatorSnapshot(&creator); err == nil {
		t.Fatal("creator Markdown larger than its business byte budget was accepted")
	}

	server := createMinecraftServerRequest{
		Address:           "example.org",
		Name:              "bounded server",
		BodyMarkdown:      strings.Repeat("a", maxServerBodyMarkdownBytes+1),
		MinecraftVersions: []string{"1.21.1"},
		Languages:         []string{"zh-CN"},
		PrimaryTag:        "survival",
	}
	if err := normalizeAndValidateServerRequest(&server, defaultServerCatalogSettings(), false); err == nil {
		t.Fatal("server Markdown larger than its business byte budget was accepted")
	}
}
