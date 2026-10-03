package antiabuse

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
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

func (s *Service) ClassifyCrawler(ctx context.Context, ip, userAgent, botToken string) (CrawlerClass, error) {
	if !s.Enabled() {
		return HumanCrawler, nil
	}
	userAgentLower := strings.ToLower(strings.TrimSpace(userAgent))
	rules, err := s.botRules(ctx)
	if err != nil {
		return SuspiciousBot, err
	}
	for _, rule := range rules {
		if rule.Kind == "blocked_bot" || rule.Kind == "ip_block" {
			if ruleMatches(rule.Matcher, ip, userAgentLower) {
				return BlockedBot, nil
			}
		}
	}
	for _, rule := range rules {
		if rule.Kind == "ip_allow" && ruleMatches(rule.Matcher, ip, userAgentLower) {
			return AllowedBot, nil
		}
	}
	if botToken != "" {
		hash := tokenHash(botToken)
		for _, rule := range rules {
			if rule.SecretHash == hash && (rule.Kind == "allowed_bot" || rule.Kind == "monitoring_bot") && ruleMatches(rule.Matcher, ip, userAgentLower) {
				if rule.Kind == "monitoring_bot" {
					return MonitoringBot, nil
				}
				return AllowedBot, nil
			}
		}
	}
	known, suffixes := knownSearchCrawler(userAgentLower)
	if known {
		if s.verifySearchEngineDNS(ctx, ip, suffixes) {
			return VerifiedSearchEngine, nil
		}
		return SuspiciousBot, nil
	}
	if crawlerUserAgent(userAgentLower) {
		return UnknownCrawler, nil
	}
	return HumanCrawler, nil
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

// ExpensiveReadLimit applies a separate budget to bounded-but-nontrivial public
// reads. Unlike crawler policy, this budget intentionally includes ordinary
// anonymous clients so rotating user agents cannot bypass database protection.
func (s *Service) ExpensiveReadLimit(ctx context.Context, scope, identity string, limit int, window time.Duration) (bool, time.Duration) {
	if s == nil || !s.Enabled() || s.cache == nil {
		return true, 0
	}
	limit = max(1, min(limit, 10_000))
	if window < time.Second {
		window = time.Second
	}
	if window > time.Hour {
		window = time.Hour
	}
	result := s.cache.ConsumeRateLimit(ctx,
		"expensive-read:"+truncate(scope, 80)+":"+s.privateHash("read", identity), limit, window)
	return result.Allowed, result.RetryAfter
}

func (s *Service) RecordCrawler(ctx context.Context, class CrawlerClass, ip, userAgent, path string, allowed bool) error {
	if class == HumanCrawler || !s.cache.ClaimThrottle(ctx, "crawler-log:"+string(class)+":"+s.privateHash("ip", ip), time.Minute) {
		return nil
	}
	outcome := AllowWithLog
	code := "crawler_read"
	if !allowed {
		outcome, code = Delay, "crawler_rate_limited"
	}
	input := Evaluation{Action: "read.crawl", IP: ip, UserAgent: userAgent, ObjectType: "path", ObjectKey: path}
	decision := Decision{Outcome: outcome, Code: code, RiskScore: 0, Rules: []string{code}}
	_, err := s.insertEvent(ctx, input, decision, class)
	if err != nil {
		s.riskEventFailed.Add(1)
	}
	return err
}

func (s *Service) botRules(ctx context.Context) ([]botRule, error) {
	if s.db == nil {
		return nil, nil
	}
	raw, err := s.cache.GetOrLoad(ctx, botRulesCacheKey, func(loadCtx context.Context) ([]byte, error) {
		values, loadErr := loadBotRules(loadCtx, s.db)
		if loadErr != nil {
			return nil, loadErr
		}
		return json.Marshal(values)
	})
	if err != nil {
		s.botRuleLoadFailed.Add(1)
		return nil, err
	}
	var values []botRule
	if err = json.Unmarshal(raw, &values); err != nil {
		s.botRuleLoadFailed.Add(1)
		return nil, err
	}
	return values, nil
}

type botRuleQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadBotRules(ctx context.Context, queryer botRuleQueryer) ([]botRule, error) {
	rows, err := queryer.Query(ctx, `select kind,matcher,secret_hash from anti_abuse_bot_rules
		where enabled and (expires_at is null or expires_at>now()) order by id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]botRule, 0)
	for rows.Next() {
		var value botRule
		if err = rows.Scan(&value.Kind, &value.Matcher, &value.SecretHash); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func ruleMatches(matcher, ip, userAgent string) bool {
	matcher = strings.ToLower(strings.TrimSpace(matcher))
	if matcher == "" {
		return true
	}
	if _, network, err := net.ParseCIDR(matcher); err == nil {
		return network.Contains(net.ParseIP(ip))
	}
	if address := net.ParseIP(matcher); address != nil {
		return address.Equal(net.ParseIP(ip))
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
	// A successful Google reverse/forward check must not authorize a Bing
	// user agent, and a negative check for one provider must not poison another.
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
