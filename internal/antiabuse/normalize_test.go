package antiabuse

import "testing"

func TestNormalizeContentRemovesUnicodeAndTrackingNoise(t *testing.T) {
	left := NormalizeContent("  Ｈｅｌｌｏ\u200b **WORLD** https://example.test/a?utm_source=x&id=7#fragment ")
	right := NormalizeContent("hello world https://example.test/a?id=7")
	if left != right {
		t.Fatalf("normalized values differ:\n%q\n%q", left, right)
	}
	if ContentHash(left) != ContentHash(right) {
		t.Fatal("equivalent content must have the same exact fingerprint")
	}
}

func TestSimilaritySeparatesNearAndUnrelatedContent(t *testing.T) {
	base := SimHash(NormalizeContent("这个教程介绍如何安装模组并配置加载器版本"))
	near := Similarity(base, SimHash(NormalizeContent("这个教程介绍如何安装模组，以及配置加载器版本")))
	unrelated := Similarity(base, SimHash(NormalizeContent("服务器今晚维护，期间无法连接")))
	if near <= unrelated {
		t.Fatalf("near similarity %d must exceed unrelated similarity %d", near, unrelated)
	}
	if near < 800 {
		t.Fatalf("minor edit similarity %d is too low for configurable near-duplicate detection", near)
	}
}
