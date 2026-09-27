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

func TestNormalizeCommunityPostSourceLocaleUsesExplicitSiteLocaleOrConfidentDetection(t *testing.T) {
	t.Run("explicit site locale is authoritative", func(t *testing.T) {
		post := communityPostSnapshot{
			Kind: "tutorial", Title: "printf setup", BodyMarkdown: "Use foo.bar()", SourceLocale: "fr_fr",
		}
		if err := normalizeCommunityPostSnapshot(&post); err != nil {
			t.Fatalf("explicit editable source locale was rejected: %v", err)
		}
		if post.SourceLocale != "fr-FR" {
			t.Fatalf("explicit source locale was overwritten: got %q, want fr-FR", post.SourceLocale)
		}
	})

	t.Run("unsupported explicit locale is rejected", func(t *testing.T) {
		post := communityPostSnapshot{
			Kind: "tutorial", Title: "설정", BodyMarkdown: "기술 문서", SourceLocale: "ko-KR",
		}
		if err := normalizeCommunityPostSnapshot(&post); err == nil {
			t.Fatal("unsupported explicit source locale was accepted")
		}
	})

	t.Run("ambiguous missing locale requires confirmation", func(t *testing.T) {
		post := communityPostSnapshot{Kind: "tutorial", Title: "printf setup", BodyMarkdown: "Use foo.bar()"}
		if err := normalizeCommunityPostSnapshot(&post); err == nil {
			t.Fatal("ambiguous content without an explicit source locale was silently classified")
		}
	})

	t.Run("missing locale still permits confident detection", func(t *testing.T) {
		post := communityPostSnapshot{Kind: "tutorial", Title: "红石教程", BodyMarkdown: "这是正文"}
		if err := normalizeCommunityPostSnapshot(&post); err != nil {
			t.Fatalf("confident script detection was rejected: %v", err)
		}
		if post.SourceLocale != "zh-CN" {
			t.Fatalf("detected source locale = %q, want zh-CN", post.SourceLocale)
		}
	})
}

func TestNormalizeIssueRequiresProjectAndVersionsButAllowsNoSmallResource(t *testing.T) {
	post := communityPostSnapshot{
		Kind: "issue", Title: "Example", SourceLocale: "en-US", BodyMarkdown: "Details", Severity: "minor",
		Projects: []communityPostReference{{Identifier: "example"}},
	}
	if err := normalizeCommunityPostSnapshot(&post); err == nil {
		t.Fatal("issue without version constraints was accepted")
	}
	post.MinecraftVersions = []string{"1.21.1"}
	post.ModVersionMin = "1.0.0"
	if err := normalizeCommunityPostSnapshot(&post); err != nil {
		t.Fatalf("issue without a small resource was rejected: %v", err)
	}
	post.Projects = nil
	if err := normalizeCommunityPostSnapshot(&post); err == nil {
		t.Fatal("issue without a project was accepted")
	}
}

func TestNormalizeNewsDropsMinecraftAndIssueFields(t *testing.T) {
	post := communityPostSnapshot{
		Kind: "news", Title: "Release news", SourceLocale: "en-US", BodyMarkdown: "Details", MinecraftVersions: []string{"1.21.1"},
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
	post := communityPostSnapshot{Kind: "discussion", Title: "How do I configure this?", SourceLocale: "en-US", BodyMarkdown: "Details", BountyCurrency: "diamond"}
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
