package antiabuse

import (
	"context"
	"testing"
)

func TestCrawlerDNSCacheSeparatesProviderVerification(t *testing.T) {
	for _, scenario := range []struct {
		name, ip string
		firstUA  string
	}{
		{"positive must not authorize another provider", "203.0.113.133", "Googlebot"},
		{"negative must not block the matching provider", "203.0.113.134", "Bingbot"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			service := crawlerTestService(fakeResolver{
				reverse: map[string][]string{scenario.ip: {"crawl.googlebot.com."}},
				forward: map[string][]string{"crawl.googlebot.com": {scenario.ip}},
			})
			for _, ua := range []string{scenario.firstUA, "Googlebot", "Bingbot", "Googlebot", "Bingbot"} {
				want := SuspiciousBot
				if ua == "Googlebot" {
					want = VerifiedSearchEngine
				}
				got, err := service.ClassifyCrawler(context.Background(), scenario.ip, ua, "")
				if err != nil || got != want {
					t.Fatalf("%s after cached provider: class=%s want=%s err=%v", ua, got, want, err)
				}
			}
		})
	}
}

func TestCrawlerIPMatchersDoNotTrustUserAgentText(t *testing.T) {
	for _, scenario := range []struct {
		matcher, ip, agent string
		want               bool
	}{
		{"203.0.113.100", "198.51.100.50", "Mozilla 203.0.113.100", false},
		{"203.0.113.0/24", "invalid", "203.0.113.0/24", false},
		{"203.0.113.100", "203.0.113.100", "Mozilla", true},
		{"203.0.113.0/24", "203.0.113.100", "Mozilla", true},
		{"2001:db8::1", "2001:db8:0:0:0:0:0:1", "Mozilla", true},
		{"examplebot", "198.51.100.50", "examplebot/1", true},
	} {
		if got := ruleMatches(scenario.matcher, scenario.ip, scenario.agent); got != scenario.want {
			t.Errorf("matcher=%q ip=%q agent=%q: got=%v want=%v", scenario.matcher, scenario.ip, scenario.agent, got, scenario.want)
		}
	}
}
