package antiabuse

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"time"
)

type botRule struct {
	Kind       string `json:"kind"`
	Matcher    string `json:"matcher"`
	SecretHash string `json:"secretHash"`
}

type dnsCacheEntry struct {
	class     CrawlerClass
	expiresAt time.Time
}

var crawlerDNSCache = struct {
	sync.Mutex
	values map[string]dnsCacheEntry
}{values: map[string]dnsCacheEntry{}}

func (s *Service) ClassifyCrawler(ctx context.Context, ip, userAgent, botToken string) CrawlerClass {
	if !s.Enabled() {
		return HumanCrawler
	}
	userAgentLower := strings.ToLower(strings.TrimSpace(userAgent))
	rules := s.botRules(ctx)
	for _, rule := range rules {
		if rule.Kind == "blocked_bot" || rule.Kind == "ip_block" {
			if ruleMatches(rule.Matcher, ip, userAgentLower) {
				return BlockedBot
			}
		}
	}
	for _, rule := range rules {
		if rule.Kind == "ip_allow" && ruleMatches(rule.Matcher, ip, userAgentLower) {
			return AllowedBot
		}
	}
	if botToken != "" {
		hash := tokenHash(botToken)
		for _, rule := range rules {
			if rule.SecretHash == hash && (rule.Kind == "allowed_bot" || rule.Kind == "monitoring_bot") && ruleMatches(rule.Matcher, ip, userAgentLower) {
				if rule.Kind == "monitoring_bot" {
					return MonitoringBot
				}
				return AllowedBot
			}
		}
	}
	known, suffixes := knownSearchCrawler(userAgentLower)
	if known {
		if s.verifySearchEngineDNS(ctx, ip, suffixes) {
			return VerifiedSearchEngine
		}
		return SuspiciousBot
	}
	if crawlerUserAgent(userAgentLower) {
		return UnknownCrawler
	}
	return HumanCrawler
}

func (s *Service) ReadLimit(ctx context.Context, class CrawlerClass, ip, path string) (bool, time.Duration) {
	if class == HumanCrawler {
		return true, 0
	}
	limit := 60
	switch class {
	case VerifiedSearchEngine:
		limit = 600
	case AllowedBot:
		limit = 300
	case MonitoringBot:
		limit = 120
	case SuspiciousBot:
		limit = 20
	case BlockedBot:
		return false, 24 * time.Hour
	}
	if strings.Contains(path, "/search") {
		limit = max(5, limit/4)
	}
	result := s.cache.ConsumeRateLimit(ctx, "crawler:"+string(class)+":"+s.privateHash("ip", ip), limit, time.Minute)
	return result.Allowed, result.RetryAfter
}

func (s *Service) RecordCrawler(ctx context.Context, class CrawlerClass, ip, userAgent, path string, allowed bool) {
	if class == HumanCrawler || !s.cache.ClaimThrottle(ctx, "crawler-log:"+string(class)+":"+s.privateHash("ip", ip), time.Minute) {
		return
	}
	outcome := AllowWithLog
	code := "crawler_read"
	if !allowed {
		outcome, code = Delay, "crawler_rate_limited"
	}
	input := Evaluation{Action: "read.crawl", IP: ip, UserAgent: userAgent, ObjectType: "path", ObjectKey: path}
	decision := Decision{Outcome: outcome, Code: code, RiskScore: 0, Rules: []string{code}}
	_, _ = s.insertEvent(ctx, input, decision, class)
}

func (s *Service) botRules(ctx context.Context) []botRule {
	if s.db == nil {
		return nil
	}
	raw, err := s.cache.GetOrLoad(ctx, botRulesCacheKey, func(loadCtx context.Context) ([]byte, error) {
		rows, err := s.db.Query(loadCtx, `select kind,matcher,secret_hash from anti_abuse_bot_rules
			where enabled and (expires_at is null or expires_at>now()) order by id`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		values := make([]botRule, 0)
		for rows.Next() {
			var value botRule
			if rows.Scan(&value.Kind, &value.Matcher, &value.SecretHash) == nil {
				values = append(values, value)
			}
		}
		return json.Marshal(values)
	})
	if err != nil {
		return nil
	}
	var values []botRule
	_ = json.Unmarshal(raw, &values)
	return values
}

func ruleMatches(matcher, ip, userAgent string) bool {
	matcher = strings.ToLower(strings.TrimSpace(matcher))
	if matcher == "" {
		return true
	}
	if parsed := net.ParseIP(ip); parsed != nil {
		if _, network, err := net.ParseCIDR(matcher); err == nil {
			return network.Contains(parsed)
		}
		if matcher == strings.ToLower(parsed.String()) {
			return true
		}
	}
	return strings.Contains(userAgent, matcher)
}

func knownSearchCrawler(userAgent string) (bool, []string) {
	switch {
	case strings.Contains(userAgent, "googlebot"):
		return true, []string{".googlebot.com", ".google.com"}
	case strings.Contains(userAgent, "bingbot"):
		return true, []string{".search.msn.com"}
	case strings.Contains(userAgent, "baiduspider"):
		return true, []string{".baidu.com", ".baidu.jp"}
	case strings.Contains(userAgent, "yandexbot"):
		return true, []string{".yandex.ru", ".yandex.net", ".yandex.com"}
	default:
		return false, nil
	}
}

func crawlerUserAgent(value string) bool {
	for _, marker := range []string{"bot", "crawler", "spider", "slurp", "httpclient", "headless", "scrapy", "uptime", "monitor"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func (s *Service) verifySearchEngineDNS(ctx context.Context, ip string, suffixes []string) bool {
	now := s.now()
	// A verdict for Google's hostnames says nothing about a Bing claim from
	// the same IP. Cache the actual verification scope, including negatives.
	cacheKey := ip + "\x00" + strings.Join(suffixes, "\x00")
	crawlerDNSCache.Lock()
	if cached, exists := crawlerDNSCache.values[cacheKey]; exists && now.Before(cached.expiresAt) {
		crawlerDNSCache.Unlock()
		return cached.class == VerifiedSearchEngine
	}
	crawlerDNSCache.Unlock()
	lookupCtx, cancel := context.WithTimeout(ctx, s.cfg.DNSLookupTimeout)
	defer cancel()
	hosts, err := s.resolver.LookupAddr(lookupCtx, ip)
	verified := false
	if err == nil {
		for _, host := range hosts {
			host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
			if !hasDNSHostnameSuffix(host, suffixes) {
				continue
			}
			addresses, forwardErr := s.resolver.LookupHost(lookupCtx, host)
			if forwardErr != nil {
				continue
			}
			for _, address := range addresses {
				if parsed := net.ParseIP(address); parsed != nil && parsed.Equal(net.ParseIP(ip)) {
					verified = true
					break
				}
			}
			if verified {
				break
			}
		}
	}
	class := SuspiciousBot
	if verified {
		class = VerifiedSearchEngine
	}
	crawlerDNSCache.Lock()
	if len(crawlerDNSCache.values) >= 2048 {
		for key := range crawlerDNSCache.values {
			delete(crawlerDNSCache.values, key)
			break
		}
	}
	crawlerDNSCache.values[cacheKey] = dnsCacheEntry{class: class, expiresAt: now.Add(s.cfg.DNSCacheTTL)}
	crawlerDNSCache.Unlock()
	return verified
}

func hasDNSHostnameSuffix(host string, suffixes []string) bool {
	for _, suffix := range suffixes {
		suffix = strings.ToLower(suffix)
		if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
			return true
		}
	}
	return false
}
