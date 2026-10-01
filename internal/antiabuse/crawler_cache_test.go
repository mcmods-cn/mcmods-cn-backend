package antiabuse

import (
	"context"
	"testing"
)

func TestCrawlerDNSCacheIsScopedToClaimedSearchEngine(t *testing.T) {
	for _, item := range []struct {
		name, ip, host, first, second string
		wantFirst, wantSecond         CrawlerClass
	}{
		{"verified Google cannot verify Bing", "203.0.113.151", "crawl.googlebot.com", "Googlebot", "Bingbot", VerifiedSearchEngine, SuspiciousBot},
		{"failed Google cannot reject valid Bing", "203.0.113.152", "crawl.search.msn.com", "Googlebot", "Bingbot", SuspiciousBot, VerifiedSearchEngine},
	} {
		t.Run(item.name, func(t *testing.T) {
			service := crawlerTestService(fakeResolver{
				reverse: map[string][]string{item.ip: {item.host + "."}},
				forward: map[string][]string{item.host: {item.ip}},
			})
			if got := service.ClassifyCrawler(context.Background(), item.ip, item.first, ""); got != item.wantFirst {
				t.Fatalf("first claimed engine: got %s, want %s", got, item.wantFirst)
			}
			if got := service.ClassifyCrawler(context.Background(), item.ip, item.second, ""); got != item.wantSecond {
				t.Fatalf("second claimed engine reused unrelated DNS verdict: got %s, want %s", got, item.wantSecond)
			}
		})
	}
}
