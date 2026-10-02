package antiabuse

import (
	"context"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

type fakeResolver struct {
	reverse map[string][]string
	forward map[string][]string
}

func (r fakeResolver) LookupAddr(_ context.Context, value string) ([]string, error) {
	return r.reverse[value], nil
}
func (r fakeResolver) LookupHost(_ context.Context, value string) ([]string, error) {
	return r.forward[value], nil
}

func crawlerTestService(resolver DNSResolver) *Service {
	cfg := config.AntiAbuseConfig{Enabled: true, IPHashSecret: "test-private-hash-secret-at-least-32-bytes", DNSLookupTimeout: time.Second, DNSCacheTTL: time.Minute}
	service := New(context.Background(), cfg, nil, querycache.New(config.RedisConfig{}))
	service.resolver = resolver
	return service
}

func TestVerifiedSearchCrawlerRequiresReverseAndForwardDNS(t *testing.T) {
	ip := "203.0.113.91"
	service := crawlerTestService(fakeResolver{reverse: map[string][]string{ip: {"crawl.googlebot.com."}}, forward: map[string][]string{"crawl.googlebot.com": {ip}}})
	if got, err := service.ClassifyCrawler(context.Background(), ip, "Mozilla Googlebot/2.1", ""); err != nil || got != VerifiedSearchEngine {
		t.Fatalf("crawler class = %s err=%v", got, err)
	}
}

func TestSpoofedSearchCrawlerIsSuspicious(t *testing.T) {
	ip := "203.0.113.92"
	service := crawlerTestService(fakeResolver{reverse: map[string][]string{ip: {"googlebot.com.attacker.test."}}, forward: map[string][]string{}})
	if got, err := service.ClassifyCrawler(context.Background(), ip, "Googlebot/2.1", ""); err != nil || got != SuspiciousBot {
		t.Fatalf("spoofed crawler class = %s err=%v", got, err)
	}
	if hasDNSHostnameSuffix("googlebot.com.attacker.test", []string{".googlebot.com"}) {
		t.Fatal("suffix boundary accepted a forged hostname")
	}
}

func TestUnknownCrawlerGetsSeparateReadBudget(t *testing.T) {
	service := crawlerTestService(fakeResolver{})
	if got, err := service.ClassifyCrawler(context.Background(), "198.51.100.4", "ExampleSpider/1.0", ""); err != nil || got != UnknownCrawler {
		t.Fatalf("unknown crawler class = %s err=%v", got, err)
	}
	for index := 0; index < 16; index++ {
		allowed, _ := service.ReadLimit(context.Background(), UnknownCrawler, "198.51.100.4", "/api/v1/search")
		if index >= 15 && allowed {
			t.Fatalf("expensive search crawler request %d unexpectedly allowed", index+1)
		}
	}
}

func TestExpensiveReadBudgetAlsoAppliesToHumanClients(t *testing.T) {
	service := crawlerTestService(fakeResolver{})
	for index := 0; index < 4; index++ {
		allowed, retry := service.ExpensiveReadLimit(context.Background(), "mod-content-cards", "198.51.100.8", 3, time.Minute)
		if index < 3 && !allowed {
			t.Fatalf("request %d was limited early", index+1)
		}
		if index == 3 && (allowed || retry <= 0) {
			t.Fatalf("fourth request allowed=%t retry=%s", allowed, retry)
		}
	}
	allowed, _ := service.ExpensiveReadLimit(context.Background(), "mod-content-graph", "198.51.100.8", 3, time.Minute)
	if !allowed {
		t.Fatal("independent expensive-read scope shared a budget")
	}
}
