package httpapi

import "testing"

func TestDetectCommunityPostLocale(t *testing.T) {
	tests := map[string]string{
		"这是一个红石机械教程":                                   "zh-CN",
		"這是一個紅石機械教學":                                   "zh-TW",
		"アイテムを追加するチュートリアル":                             "ja-JP",
		"Это руководство по установке мода":            "ru-RU",
		"Ceci est un tutoriel pour installer le mod":   "fr-FR",
		"Esta es una guía para instalar el mod":        "es-ES",
		"Dies ist eine Anleitung für die Installation": "de-DE",
		"This tutorial explains how to install a mod":  "en-US",
	}
	for input, expected := range tests {
		if actual := detectCommunityPostLocale(input); actual != expected {
			t.Errorf("detectCommunityPostLocale(%q)=%q, want %q", input, actual, expected)
		}
	}
}

func TestNormalizeIssueRequiresReferencesAndVersions(t *testing.T) {
	post := communityPostSnapshot{
		Kind: "issue", Title: "Example", BodyMarkdown: "Details", Severity: "minor",
		Projects:  []communityPostReference{{Identifier: "example"}},
		Resources: []communityPostReference{{Identifier: "example:item", Kind: "minecraft.item"}},
	}
	if err := normalizeCommunityPostSnapshot(&post); err == nil {
		t.Fatal("issue without version constraints was accepted")
	}
	post.MinecraftVersions = []string{"1.21.1"}
	post.ModVersionMin = "1.0.0"
	if err := normalizeCommunityPostSnapshot(&post); err != nil {
		t.Fatalf("valid issue was rejected: %v", err)
	}
}

func TestNormalizeNewsDropsMinecraftAndIssueFields(t *testing.T) {
	post := communityPostSnapshot{
		Kind: "news", Title: "Release news", BodyMarkdown: "Details", MinecraftVersions: []string{"1.21.1"},
		Severity: "fatal", ModVersionMin: "1.0", IssueURL: "https://example.com/issue", HasFix: true,
		BountyCurrency: "diamond", BountyAmount: 10,
	}
	if err := normalizeCommunityPostSnapshot(&post); err != nil {
		t.Fatalf("valid news was rejected: %v", err)
	}
	if len(post.MinecraftVersions) != 0 || post.Severity != "" || post.ModVersionMin != "" || post.IssueURL != "" || post.HasFix || post.BountyAmount != 0 {
		t.Fatalf("news retained unrelated fields: %#v", post)
	}
}

func TestNormalizeDiscussionBountyPair(t *testing.T) {
	post := communityPostSnapshot{Kind: "discussion", Title: "How do I configure this?", BodyMarkdown: "Details", BountyCurrency: "diamond"}
	if err := normalizeCommunityPostSnapshot(&post); err == nil {
		t.Fatal("discussion with currency but no bounty amount was accepted")
	}
	post.BountyAmount = 25
	if err := normalizeCommunityPostSnapshot(&post); err != nil {
		t.Fatalf("valid bounty question was rejected: %v", err)
	}
}

func TestCommunityPostAuthorCommentModeration(t *testing.T) {
	for _, kind := range []string{"tutorial", "discussion"} {
		if !communityPostAuthorModeratesComments(kind) {
			t.Fatalf("%s author should moderate its comments", kind)
		}
	}
	for _, kind := range []string{"news", "issue"} {
		if communityPostAuthorModeratesComments(kind) {
			t.Fatalf("%s author must not gain automatic comment moderation", kind)
		}
	}
}
