package antiabuse

import (
	"strings"
	"testing"
)

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

func TestSimHashPreservesStoredFingerprints(t *testing.T) {
	for _, sample := range []struct {
		content string
		want    uint64
	}{
		{"", 0x0000000000000000},
		{"  ", 0xffffffffffffffff},
		{"x", 0x44b026b74216712d},
		{"ab", 0x6e7f7d2efee8afff},
		{"abc", 0xe2ea41a92f3c793e},
		{"abcdef", 0xd6eb9236ad79a2ae},
		{"测试模组 和 资源", 0xa93dbc67e8f4a6ba},
		{"ababbaba", 0x88bcfbeccc9d5fe1},
		{"😃木😃木 😃", 0x30ae7bffffc57a9d},
		{"abcd   abcd", 0xe9bf504aff31bcba},
	} {
		if got := SimHash(sample.content); got != sample.want {
			t.Errorf("SimHash(%q) = %016x, want %016x", sample.content, got, sample.want)
		}
	}
}
func BenchmarkSimHash(b *testing.B) {
	text := strings.Repeat("社区教程 提取 Minecraft 模组资源，包含 Forge 和 Fabric 的配置。 ", 70)
	b.ReportAllocs()
	for b.Loop() {
		SimHash(text)
	}
}
