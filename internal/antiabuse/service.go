package antiabuse

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

const settingsKey = "anti_abuse.config"

const (
	accountCacheKeyPrefix = "anti-abuse:account:"
	botRulesCacheKey      = "anti-abuse:bot-rules"
	settingsCacheKey      = "anti-abuse:settings"
)

type Service struct {
	cfg      config.AntiAbuseConfig
	db       *pgxpool.Pool
	cache    *querycache.Cache
	resolver DNSResolver
	now      func() time.Time
	events   chan queuedRiskEvent
}

type queuedRiskEvent struct {
	input         Evaluation
	decision      Decision
	crawler       CrawlerClass
	aggregateOnly bool
}

type riskAggregate struct {
	date, action, outcome, crawler string
	count                          int64
}

type DNSResolver interface {
	LookupAddr(context.Context, string) ([]string, error)
	LookupHost(context.Context, string) ([]string, error)
}

type netResolver struct{ value *net.Resolver }

func (r netResolver) LookupAddr(ctx context.Context, value string) ([]string, error) {
	return r.value.LookupAddr(ctx, value)
}
func (r netResolver) LookupHost(ctx context.Context, value string) ([]string, error) {
	return r.value.LookupHost(ctx, value)
}

func New(cfg config.AntiAbuseConfig, db *pgxpool.Pool, cache *querycache.Cache) *Service {
	service := &Service{cfg: cfg, db: db, cache: cache, resolver: netResolver{net.DefaultResolver}, now: time.Now, events: make(chan queuedRiskEvent, 2048)}
	if cfg.Enabled && db != nil {
		go service.writeRiskEvents()
	}
	return service
}

func (s *Service) Enabled() bool { return s != nil && s.cfg.Enabled }

type accountProfile struct {
	CreatedAt              time.Time `json:"createdAt"`
	EmailVerified          bool      `json:"emailVerified"`
	Status                 string    `json:"status"`
	Level                  int       `json:"level"`
	TrustLevel             string    `json:"trustLevel"`
	RiskScore              int       `json:"riskScore"`
	ManuallyTrusted        bool      `json:"manuallyTrusted"`
	ChallengeRequiredUntil time.Time `json:"challengeRequiredUntil"`
	ReviewRequiredUntil    time.Time `json:"reviewRequiredUntil"`
	RestrictedUntil        time.Time `json:"restrictedUntil"`
	RestrictionMode        string    `json:"restrictionMode"`
	RestrictionActions     []string  `json:"restrictionActions"`
	RestrictionEnd         time.Time `json:"restrictionEnd"`
}

func (s *Service) Evaluate(ctx context.Context, input Evaluation) (Decision, error) {
	decision := Decision{Outcome: Allow, Code: "allowed", Message: "request allowed"}
	if !s.Enabled() || input.Action == "" {
		return decision, nil
	}
	if input.Now.IsZero() {
		input.Now = s.now()
	}
	// Comment handlers persist idempotency keys under a database uniqueness
	// constraint. Only a key already committed by the business handler is a
	// replay; a key seen merely by this middleware must never bypass a challenge.
	if strings.HasPrefix(input.Action, "comment.") && strings.TrimSpace(input.IdempotencyKey) != "" && s.db != nil {
		var committed bool
		if err := s.db.QueryRow(ctx, `select exists(select 1 from comments where author_id=$1 and idempotency_key=$2)`, input.UserID, strings.TrimSpace(input.IdempotencyKey)).Scan(&committed); err == nil && committed {
			return decision, nil
		}
	}
	settings, err := s.Settings(ctx)
	if err != nil {
		// Fail soft for low-risk authenticated traffic. Rate limiting continues
		// with environment defaults and the Redis/local atomic counter.
		settings = DefaultSettings()
	}
	if !settings.Enabled {
		return decision, nil
	}
	ipHash := s.privateHash("ip", input.IP)
	subnetHash := s.privateHash("subnet", subnetForIP(input.IP))
	deviceHash := s.privateHash("device", input.DeviceID)
	sessionHash := s.privateHash("session", input.SessionID)
	profile, profileErr := s.account(ctx, input.UserID, ipHash, deviceHash)
	if profileErr != nil && !errors.Is(profileErr, pgx.ErrNoRows) {
		return decision, profileErr
	}
	trust := classifyTrust(profile, settings, input.Now)
	if profile.Status != "" && profile.Status != "active" {
		decision = Decision{Outcome: Deny, Code: "account_unavailable", Message: "当前账户暂时无法执行该操作", RiskScore: 100, Rules: []string{"account_status"}}
		return decision, nil
	}
	if input.CrawlerClass == VerifiedSearchEngine || input.CrawlerClass == AllowedBot || input.CrawlerClass == MonitoringBot || input.CrawlerClass == BlockedBot {
		return Decision{Outcome: Deny, Code: "crawler_write_forbidden", Message: "只读机器人不能执行写操作", RiskScore: 100, Rules: []string{"crawler_write_forbidden"}}, nil
	}
	if restrictionApplies(profile, input.Action, input.Now) {
		outcome := TempBlock
		if profile.RestrictionMode == "permanent_ban" {
			outcome = Deny
		}
		decision = Decision{Outcome: outcome, Code: "action_restricted", Message: "当前账户暂时无法执行该操作", RiskScore: max(profile.RiskScore, 80), Rules: []string{"active_restriction"}, RetryAfter: time.Until(profile.RestrictionEnd)}
		return decision, nil
	}

	score := profile.RiskScore / 10
	rules := make([]string, 0, 8)
	if trust == "new" {
		score += 15
		rules = append(rules, "new_account")
	}
	if !profile.EmailVerified {
		score += 8
		rules = append(rules, "email_unverified")
	}
	if trust == "high_risk" || trust == "restricted" {
		score += 30
		rules = append(rules, "account_risk_state")
	}
	if settings.EmergencyMode && trust != "trusted" {
		score += 25
		rules = append(rules, "emergency_mode")
	}
	if strings.TrimSpace(input.Honeypot) != "" {
		score += 80
		rules = append(rules, "honeypot_filled")
	}
	if suspiciousUserAgent(input.UserAgent) {
		score += 12
		rules = append(rules, "suspicious_client")
	}
	if input.CrawlerClass != "" && input.CrawlerClass != HumanCrawler {
		score += 35
		rules = append(rules, "automated_write_client")
	}

	if input.FormToken != "" {
		age, valid := s.consumeFormToken(ctx, input, sessionHash, ipHash)
		if !valid {
			score += 50
			rules = append(rules, "invalid_form_token")
		} else if age < s.cfg.FormMinimumAge {
			score += 18
			rules = append(rules, "form_submitted_too_fast")
		}
	} else if contentAction(input.Action) && trust == "new" {
		score += 12
		rules = append(rules, "missing_form_token")
	}

	challengePassed := false
	challengeUnavailable := false
	if input.ChallengeProof != "" {
		var available bool
		challengePassed, available = s.verifyChallenge(ctx, input, sessionHash, ipHash)
		if challengePassed {
			score = max(0, score-60)
			rules = append(rules, "challenge_passed")
		} else if available {
			score += 30
			rules = append(rules, "challenge_failed")
		} else {
			challengeUnavailable = true
			score = max(score, settings.ModerationThreshold)
			rules = append(rules, "challenge_provider_unavailable")
		}
	}

	policy := policyFor(settings, input.Action, trust, input.RateLimitPercent)
	if !challengePassed {
		limited, retry, limitRule := s.applyLimits(ctx, input, policy, ipHash, subnetHash, deviceHash, sessionHash)
		if limited {
			decision = Decision{Outcome: Delay, Code: "rate_limited", Message: "请求过于频繁，请稍后重试", RiskScore: max(score, 45), Rules: append(rules, limitRule), RetryAfter: retry}
			return decision, nil
		}
	}
	if policy.PendingLimit > 0 && strings.Contains(input.Action, "submit") {
		pending, pendingErr := s.pendingReviewCount(ctx, input.UserID)
		if pendingErr == nil && pending >= policy.PendingLimit {
			decision = Decision{Outcome: Delay, Code: "pending_review_limit", Message: "当前待审核内容过多，请等待已有内容完成审核", RiskScore: max(score, 55), Rules: append(rules, "pending_review_quota"), RetryAfter: time.Hour}
			return decision, nil
		}
	}

	normalized := NormalizeContent(input.Content)
	if !challengePassed && normalized != "" && len([]rune(normalized)) >= 3 {
		duplicateScore, duplicateRules, exactDuplicate, duplicateErr := s.duplicateRisk(ctx, input, normalized, ipHash, settings)
		if duplicateErr == nil {
			score += duplicateScore
			rules = append(rules, duplicateRules...)
			if exactDuplicate {
				decision = Decision{Outcome: Deny, Code: "duplicate_content", Message: "内容与近期提交重复，请勿重复发布", RiskScore: max(score, 100), Rules: rules}
				return decision, nil
			}
		}
	}
	if profile.ChallengeRequiredUntil.After(input.Now) && !challengePassed {
		score = max(score, settings.ChallengeThreshold)
		rules = append(rules, "challenge_required_state")
	}
	if profile.ReviewRequiredUntil.After(input.Now) {
		score = max(score, settings.ModerationThreshold)
		rules = append(rules, "moderation_required_state")
	}
	if restrictionMatchesAction(profile, input.Action, input.Now) {
		switch profile.RestrictionMode {
		case "challenge":
			if !challengePassed {
				score = max(score, settings.ChallengeThreshold)
				rules = append(rules, "administrator_challenge_required")
			}
		case "moderation":
			score = max(score, settings.ModerationThreshold)
			rules = append(rules, "administrator_moderation_required")
		}
	}

	decision = mapDecision(score, rules, settings)
	if challengePassed && decision.Outcome == Allow {
		decision.Outcome, decision.Code = AllowWithLog, "challenge_passed"
	}
	if challengeUnavailable {
		decision = Decision{Outcome: Moderation, Code: "moderation_required", Message: "验证服务暂时不可用，提交已进入审核", RiskScore: score, Rules: compactStrings(rules), Moderation: true}
	}
	if input.Administrator && decision.Outcome != Deny {
		decision.Outcome = AllowWithLog
		decision.Code = "administrator_allowed"
		decision.Message = "request allowed"
		decision.Moderation = false
	}
	if decision.Outcome == Challenge {
		if s.cfg.ChallengeProvider == "disabled" {
			decision = Decision{Outcome: Moderation, Code: "moderation_required", Message: "提交已进入人工审核", RiskScore: score, Rules: compactStrings(rules), Moderation: true}
		} else if challenge, challengeErr := s.createChallenge(ctx, input, sessionHash, ipHash); challengeErr == nil {
			decision.Challenge = &challenge
		} else {
			decision = Decision{Outcome: Moderation, Code: "moderation_required", Message: "验证服务暂时不可用，提交已进入审核", RiskScore: score, Rules: append(compactStrings(rules), "challenge_issue_failed"), Moderation: true}
		}
	}
	return decision, nil
}

func mapDecision(score int, rules []string, settings Settings) Decision {
	decision := Decision{Outcome: Allow, Code: "allowed", Message: "request allowed", RiskScore: score, Rules: compactStrings(rules)}
	switch {
	case score >= settings.DenyThreshold:
		decision.Outcome, decision.Code, decision.Message = Deny, "request_denied", "当前请求无法完成"
	case score >= settings.TempBlockThreshold:
		decision.Outcome, decision.Code, decision.Message = TempBlock, "temporarily_blocked", "当前操作已被临时限制，请稍后重试"
	case score >= settings.ChallengeThreshold:
		decision.Outcome, decision.Code, decision.Message = Challenge, "challenge_required", "请先完成人机验证"
	case score >= settings.ModerationThreshold:
		decision.Outcome, decision.Code, decision.Message, decision.Moderation = Moderation, "moderation_required", "提交已进入人工审核", true
	case score >= settings.LogThreshold:
		decision.Outcome, decision.Code = AllowWithLog, "allowed_with_monitoring"
	}
	return decision
}

func (s *Service) applyLimits(ctx context.Context, input Evaluation, policy ActionPolicy, ipHash, subnetHash, deviceHash, sessionHash string) (bool, time.Duration, string) {
	limitNamespace := rateLimitNamespace(input)
	type dimension struct {
		name, value string
		multiplier  int
	}
	dimensions := []dimension{{"user", strconv.FormatInt(input.UserID, 10), 1}, {"session", sessionHash, 1}, {"ip", ipHash, 4}, {"subnet", subnetHash, 8}, {"device", deviceHash, 2}}
	checks := []struct {
		label  string
		limit  int
		window time.Duration
	}{
		{"burst", policy.BurstLimit, time.Duration(policy.BurstSeconds) * time.Second},
		{"hour", policy.HourLimit, time.Hour}, {"day", policy.DayLimit, 24 * time.Hour},
	}
	for _, check := range checks {
		if check.limit <= 0 || check.window <= 0 {
			continue
		}
		for _, dim := range dimensions {
			if dim.value == "" || dim.value == "0" {
				continue
			}
			result := s.cache.ConsumeRateLimit(ctx, "abuse:"+limitNamespace+":"+check.label+":"+dim.name+":"+dim.value, check.limit*dim.multiplier, check.window)
			if !result.Allowed {
				return true, result.RetryAfter, "rate_" + check.label + "_" + dim.name
			}
		}
	}
	if policy.ObjectLimit > 0 && input.ObjectKey != "" {
		result := s.cache.ConsumeRateLimit(ctx, "abuse:"+limitNamespace+":object:"+strconv.FormatInt(input.UserID, 10)+":"+s.privateHash("object", input.ObjectKey), policy.ObjectLimit, time.Duration(policy.ObjectMinutes)*time.Minute)
		if !result.Allowed {
			return true, result.RetryAfter, "rate_object_user"
		}
	}
	return false, 0, ""
}

func rateLimitNamespace(input Evaluation) string {
	scope := strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '-' {
			return character
		}
		return -1
	}, strings.ToLower(strings.TrimSpace(input.RateScope)))
	if scope == "" {
		return input.Action
	}
	return input.Action + ":" + scope
}

func policyFor(settings Settings, action, trust string, rateLimitPercent int) ActionPolicy {
	policy, ok := settings.Policies[action]
	if !ok {
		policy = settings.Policies["write.generic"]
	}
	factorNumerator, factorDenominator := 1, 1
	switch trust {
	case "new", "high_risk", "restricted":
		factorNumerator, factorDenominator = 1, 2
	}
	scale := func(value int) int {
		if value <= 0 {
			return 0
		}
		return max(1, value*factorNumerator/factorDenominator)
	}
	policy.BurstLimit, policy.HourLimit, policy.DayLimit = scale(policy.BurstLimit), scale(policy.HourLimit), scale(policy.DayLimit)
	policy.ObjectLimit, policy.PendingLimit = scale(policy.ObjectLimit), scale(policy.PendingLimit)
	rateLimitPercent = NormalizeRateLimitPercent(rateLimitPercent)
	scaleByPermission := func(value int) int {
		if value <= 0 {
			return 0
		}
		// Round up so increases remain meaningful for small burst/object limits.
		return max(1, (value*rateLimitPercent+99)/100)
	}
	policy.BurstLimit = scaleByPermission(policy.BurstLimit)
	policy.HourLimit = scaleByPermission(policy.HourLimit)
	policy.DayLimit = scaleByPermission(policy.DayLimit)
	policy.ObjectLimit = scaleByPermission(policy.ObjectLimit)
	return policy
}

const (
	DefaultRateLimitPercent = 100
	MinRateLimitPercent     = 25
	MaxRateLimitPercent     = 1000
)

// NormalizeRateLimitPercent bounds permission-controlled quotas. A missing
// permission must never disable or accidentally relax rate limiting.
func NormalizeRateLimitPercent(value int) int {
	if value == 0 {
		return DefaultRateLimitPercent
	}
	return min(MaxRateLimitPercent, max(MinRateLimitPercent, value))
}

func (s *Service) duplicateRisk(ctx context.Context, input Evaluation, normalized, ipHash string, settings Settings) (int, []string, bool, error) {
	exact := ContentHash(normalized)
	simhash := SimHash(normalized)
	// Close the race in which two identical requests pass the database lookup
	// before either request commits its fingerprint. Redis makes this atomic
	// across instances; the bounded local cache is the single-instance fallback.
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		claimKey := s.privateHash("content-claim", strconv.FormatInt(input.UserID, 10)+":"+input.Action+":"+exact)
		if !s.cache.ClaimThrottle(ctx, "anti-abuse:content-claim:"+claimKey, 30*time.Second) {
			return 100, []string{"concurrent_exact_duplicate"}, true, nil
		}
	}
	if s.db == nil {
		return 0, nil, false, nil
	}
	rows, err := s.db.Query(ctx, `select user_id,exact_hash,simhash from anti_abuse_content_fingerprints
		where action=$1 and created_at>now()-make_interval(hours=>$2::int)
		  and (user_id=$3 or ($4<>'' and ip_hash=$4) or ($5<>'' and object_key=$5))
		order by created_at desc,id desc limit 200`, input.Action, settings.DuplicateWindowHours, input.UserID, ipHash, input.ObjectKey)
	if err != nil {
		return 0, nil, false, err
	}
	defer rows.Close()
	best := 0
	for rows.Next() {
		var userID *int64
		var storedHash string
		var storedSimhash int64
		if rows.Scan(&userID, &storedHash, &storedSimhash) != nil {
			continue
		}
		if storedHash == exact {
			if userID != nil && *userID == input.UserID {
				return 100, []string{"exact_duplicate_user"}, true, nil
			}
			return 45, []string{"exact_duplicate_network_or_object"}, false, nil
		}
		similarity := Similarity(simhash, uint64(storedSimhash))
		best = max(best, similarity)
	}
	if best >= settings.SimilarityThreshold && len([]rune(normalized)) >= 20 {
		return 35, []string{"near_duplicate_content"}, false, nil
	}
	return 0, nil, false, rows.Err()
}

func (s *Service) RecordDecision(ctx context.Context, input Evaluation, decision Decision, crawler CrawlerClass) {
	if !s.Enabled() || decision.Outcome == Allow {
		return
	}
	if decision.Outcome == TempBlock || decision.Outcome == AccountReview {
		s.applyAutomaticRestriction(ctx, input, decision)
	}
	// Rejected traffic must not hold an HTTP connection while a remote or busy
	// database persists security telemetry. The bounded queue applies memory
	// backpressure; restrictions above still take effect before the response.
	input.Content = truncate(input.Content, 20000)
	aggregateOnly := false
	if decision.Outcome != AccountReview {
		sampleKey := "anti-abuse:event-sample:" + s.privateHash("event-sample", strconv.FormatInt(input.UserID, 10)+":"+input.Action+":"+string(decision.Outcome)+":"+firstString(decision.Rules)+":"+input.IP)
		aggregateOnly = !s.cache.ClaimThrottle(context.Background(), sampleKey, time.Second)
	}
	select {
	case s.events <- queuedRiskEvent{input: input, decision: decision, crawler: crawler, aggregateOnly: aggregateOnly}:
	default:
		// Preserve aggregate visibility when an attack exceeds the bounded event
		// queue without spawning an unbounded goroutine per rejected request.
		if s.cache.ClaimThrottle(context.Background(), "anti-abuse:event-queue-full", time.Minute) {
			go func() {
				overflow := Evaluation{Action: "system.event_queue", ObjectType: "anti_abuse"}
				_, _ = s.insertEvent(context.Background(), overflow, Decision{Outcome: AllowWithLog, Code: "event_queue_full", RiskScore: 0, Rules: []string{"event_queue_full"}}, "")
			}()
		}
	}
}

func (s *Service) writeRiskEvents() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	aggregates := map[string]riskAggregate{}
	flush := func() {
		for key, value := range aggregates {
			_, _ = s.db.Exec(context.Background(), `insert into anti_abuse_daily_stats(stat_date,action,outcome,crawler_class,event_count)
				values($1,$2,$3,$4,$5) on conflict(stat_date,action,outcome,crawler_class) do update
				set event_count=anti_abuse_daily_stats.event_count+excluded.event_count`, value.date, value.action, value.outcome, value.crawler, value.count)
			delete(aggregates, key)
		}
	}
	for {
		select {
		case event := <-s.events:
			if !event.aggregateOnly {
				_, _ = s.insertEvent(context.Background(), event.input, event.decision, event.crawler)
				continue
			}
			date := s.now().UTC().Format("2006-01-02")
			key := date + "\x00" + event.input.Action + "\x00" + string(event.decision.Outcome) + "\x00" + string(event.crawler)
			value := aggregates[key]
			value.date, value.action, value.outcome, value.crawler = date, event.input.Action, string(event.decision.Outcome), string(event.crawler)
			value.count++
			aggregates[key] = value
		case <-ticker.C:
			flush()
		}
	}
}

func (s *Service) RecordSuccess(ctx context.Context, input Evaluation, decision Decision) {
	if !s.Enabled() {
		return
	}
	var eventID *int64
	if decision.Outcome != Allow {
		if id, err := s.insertEvent(ctx, input, decision, ""); err == nil {
			eventID = &id
		}
	}
	normalized := NormalizeContent(input.Content)
	if normalized != "" && contentAction(input.Action) {
		_, _ = s.db.Exec(ctx, `insert into anti_abuse_content_fingerprints(user_id,action,object_key,exact_hash,simhash,ip_hash,event_id)
			values($1,$2,$3,$4,$5,$6,$7)`, input.UserID, input.Action, truncate(input.ObjectKey, 160), ContentHash(normalized), int64(SimHash(normalized)), s.privateHash("ip", input.IP), eventID)
	}
	if s.cache.ClaimThrottle(ctx, "anti-abuse-cleanup", 6*time.Hour) {
		go s.cleanup(context.Background())
	}
}

func (s *Service) insertEvent(ctx context.Context, input Evaluation, decision Decision, crawler CrawlerClass) (int64, error) {
	timeout, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var id int64
	err := s.db.QueryRow(timeout, `insert into anti_abuse_events(user_id,action,object_type,object_key,outcome,risk_score,rule_codes,
		ip_hash,subnet_hash,device_hash,session_hash,crawler_class,content_hash,request_id,metadata)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'{}'::jsonb) returning id`, nullableID(input.UserID), input.Action,
		truncate(input.ObjectType, 80), truncate(input.ObjectKey, 160), decision.Outcome, decision.RiskScore, decision.Rules,
		s.privateHash("ip", input.IP), s.privateHash("subnet", subnetForIP(input.IP)), s.privateHash("device", input.DeviceID),
		s.privateHash("session", input.SessionID), crawler, contentHash(input.Content), truncate(input.RequestID, 128)).Scan(&id)
	return id, err
}

func (s *Service) applyAutomaticRestriction(ctx context.Context, input Evaluation, decision Decision) {
	if input.UserID <= 0 {
		return
	}
	settings, _ := s.Settings(ctx)
	endsAt := s.now().Add(time.Duration(settings.TemporaryBlockMinutes) * time.Minute)
	_, _ = s.db.Exec(ctx, `insert into anti_abuse_restrictions(user_id,actions,mode,source,rule_code,risk_score,reason,automatic,ends_at)
		values($1,array[$2],'cooldown','automatic',$3,$4,'Automated temporary anti-abuse restriction',true,$5)`, input.UserID, input.Action, firstString(decision.Rules), decision.RiskScore, endsAt)
	_, _ = s.db.Exec(ctx, `insert into anti_abuse_user_states(user_id,trust_level,risk_score,hit_count,restricted_until,last_event_at)
		values($1,'restricted',$2,1,$3,now()) on conflict(user_id) do update set trust_level='restricted',
		risk_score=greatest(anti_abuse_user_states.risk_score,excluded.risk_score),hit_count=anti_abuse_user_states.hit_count+1,
		restricted_until=greatest(anti_abuse_user_states.restricted_until,excluded.restricted_until),last_event_at=now(),updated_at=now()`, input.UserID, decision.RiskScore, endsAt)
	s.InvalidateAccountState(ctx, input.UserID)
}

// InvalidateAccountState centralizes the account-risk cache key. The suffix
// varies by network and client signal, so invalidating one account intentionally
// removes all of that account's short-lived variants without scanning others.
func (s *Service) InvalidateAccountState(ctx context.Context, userID int64) {
	if s == nil || s.cache == nil || userID <= 0 {
		return
	}
	s.cache.InvalidatePrefix(ctx, accountCacheKeyPrefix+strconv.FormatInt(userID, 10)+":")
}

func (s *Service) InvalidateBotRules(ctx context.Context) {
	if s == nil || s.cache == nil {
		return
	}
	s.cache.Delete(ctx, botRulesCacheKey)
}

func (s *Service) Settings(ctx context.Context) (Settings, error) {
	defaults := DefaultSettings()
	if s.db == nil {
		return defaults, nil
	}
	raw, err := s.cache.GetOrLoad(ctx, settingsCacheKey, func(loadCtx context.Context) ([]byte, error) {
		var stored []byte
		err := s.db.QueryRow(loadCtx, `select value from system_settings where key=$1`, settingsKey).Scan(&stored)
		if errors.Is(err, pgx.ErrNoRows) {
			return json.Marshal(defaults)
		}
		return stored, err
	})
	if err != nil {
		return defaults, err
	}
	var settings Settings
	if json.Unmarshal(raw, &settings) != nil {
		return defaults, errors.New("invalid anti-abuse settings")
	}
	return NormalizeSettings(settings), nil
}

func NormalizeSettings(value Settings) Settings {
	defaults := DefaultSettings()
	if value.Policies == nil {
		value.Policies = map[string]ActionPolicy{}
	}
	for key, policy := range defaults.Policies {
		if _, exists := value.Policies[key]; !exists {
			value.Policies[key] = policy
		}
	}
	value.LogThreshold = bounded(value.LogThreshold, 1, 100, defaults.LogThreshold)
	value.ModerationThreshold = bounded(value.ModerationThreshold, value.LogThreshold, 200, defaults.ModerationThreshold)
	value.ChallengeThreshold = bounded(value.ChallengeThreshold, value.ModerationThreshold, 300, defaults.ChallengeThreshold)
	value.TempBlockThreshold = bounded(value.TempBlockThreshold, value.ChallengeThreshold, 500, defaults.TempBlockThreshold)
	value.DenyThreshold = bounded(value.DenyThreshold, value.TempBlockThreshold, 1000, defaults.DenyThreshold)
	value.NewAccountDays = bounded(value.NewAccountDays, 1, 90, defaults.NewAccountDays)
	value.TrustedAccountDays = bounded(value.TrustedAccountDays, value.NewAccountDays, 3650, defaults.TrustedAccountDays)
	value.TrustedMinimumLevel = bounded(value.TrustedMinimumLevel, 0, 1000, defaults.TrustedMinimumLevel)
	value.DuplicateWindowHours = bounded(value.DuplicateWindowHours, 1, 720, defaults.DuplicateWindowHours)
	value.SimilarityThreshold = bounded(value.SimilarityThreshold, 700, 1000, defaults.SimilarityThreshold)
	value.TemporaryBlockMinutes = bounded(value.TemporaryBlockMinutes, 1, 43200, defaults.TemporaryBlockMinutes)
	for action, policy := range value.Policies {
		policy.BurstLimit = bounded(policy.BurstLimit, 1, 10000, defaults.Policies["write.generic"].BurstLimit)
		policy.BurstSeconds = bounded(policy.BurstSeconds, 1, 3600, defaults.Policies["write.generic"].BurstSeconds)
		policy.HourLimit = bounded(policy.HourLimit, 1, 100000, defaults.Policies["write.generic"].HourLimit)
		policy.DayLimit = bounded(policy.DayLimit, 1, 1000000, defaults.Policies["write.generic"].DayLimit)
		policy.ObjectLimit = bounded(policy.ObjectLimit, 1, 10000, defaults.Policies["write.generic"].ObjectLimit)
		policy.ObjectMinutes = bounded(policy.ObjectMinutes, 1, 10080, defaults.Policies["write.generic"].ObjectMinutes)
		policy.PendingLimit = boundedAllowZero(policy.PendingLimit, 10000)
		value.Policies[action] = policy
	}
	return value
}

func ValidateSettings(value Settings) error {
	if value.LogThreshold < 1 || value.ModerationThreshold < value.LogThreshold || value.ChallengeThreshold < value.ModerationThreshold ||
		value.TempBlockThreshold < value.ChallengeThreshold || value.DenyThreshold < value.TempBlockThreshold || value.DenyThreshold > 1000 {
		return errors.New("anti-abuse thresholds must be ordered and between 1 and 1000")
	}
	if value.SimilarityThreshold < 700 || value.SimilarityThreshold > 1000 || value.DuplicateWindowHours < 1 || value.DuplicateWindowHours > 720 ||
		value.NewAccountDays < 1 || value.NewAccountDays > 90 || value.TrustedAccountDays < value.NewAccountDays || value.TrustedAccountDays > 3650 ||
		value.TemporaryBlockMinutes < 1 || value.TemporaryBlockMinutes > 43200 {
		return errors.New("anti-abuse configuration is outside supported bounds")
	}
	allowed := DefaultSettings().Policies
	for action, policy := range value.Policies {
		if _, ok := allowed[action]; !ok {
			return fmt.Errorf("unsupported anti-abuse action policy %q", action)
		}
		if policy.BurstLimit < 1 || policy.BurstLimit > 10000 || policy.BurstSeconds < 1 || policy.BurstSeconds > 3600 ||
			policy.HourLimit < 1 || policy.HourLimit > 100000 || policy.DayLimit < 1 || policy.DayLimit > 1000000 ||
			policy.ObjectLimit < 1 || policy.ObjectLimit > 10000 || policy.ObjectMinutes < 1 || policy.ObjectMinutes > 10080 ||
			policy.PendingLimit < 0 || policy.PendingLimit > 10000 {
			return fmt.Errorf("anti-abuse policy %q is outside supported bounds", action)
		}
	}
	return nil
}

func (s *Service) SaveSettings(ctx context.Context, settings Settings, actorID int64) (Settings, error) {
	if err := ValidateSettings(settings); err != nil {
		return settings, err
	}
	settings = NormalizeSettings(settings)
	raw, err := json.Marshal(settings)
	if err != nil {
		return settings, err
	}
	_, err = s.db.Exec(ctx, `insert into system_settings(key,value,updated_by,updated_at) values($1,$2::jsonb,$3,now())
		on conflict(key) do update set value=excluded.value,updated_by=excluded.updated_by,updated_at=now()`, settingsKey, raw, actorID)
	if err == nil {
		s.cache.Delete(ctx, settingsCacheKey)
	}
	return settings, err
}

func (s *Service) account(ctx context.Context, userID int64, ipHash, deviceHash string) (accountProfile, error) {
	if userID <= 0 {
		return accountProfile{}, pgx.ErrNoRows
	}
	key := accountCacheKeyPrefix + strconv.FormatInt(userID, 10) + ":" + truncate(ipHash, 16) + ":" + truncate(deviceHash, 16)
	raw, err := s.cache.GetOrLoad(ctx, key, func(loadCtx context.Context) ([]byte, error) {
		var profile accountProfile
		err := s.db.QueryRow(loadCtx, `select account.created_at,account.email_verified,account.status,coalesce(experience.level,0),
			coalesce(state.trust_level,'normal'),coalesce(state.risk_score,0),coalesce(state.manually_trusted,false),
			coalesce(state.challenge_required_until,'epoch'),coalesce(state.review_required_until,'epoch'),coalesce(state.restricted_until,'epoch'),
			coalesce(restriction.mode,''),coalesce(restriction.actions,'{}'::text[]),coalesce(restriction.ends_at,now()+interval '100 years')
			from users account left join user_experience experience on experience.user_id=account.id
			left join anti_abuse_user_states state on state.user_id=account.id
			left join lateral (select mode,actions,ends_at from anti_abuse_restrictions restriction
				where restriction.lifted_at is null and restriction.starts_at<=now() and (restriction.ends_at is null or restriction.ends_at>now())
				and (restriction.user_id=account.id or ($2<>'' and restriction.ip_hash=$2) or ($3<>'' and restriction.device_hash=$3))
				order by case mode when 'permanent_ban' then 1 when 'temporary_ban' then 2 when 'read_only' then 3 else 4 end,starts_at desc limit 1) restriction on true
			where account.id=$1`, userID, ipHash, deviceHash).Scan(&profile.CreatedAt, &profile.EmailVerified, &profile.Status, &profile.Level,
			&profile.TrustLevel, &profile.RiskScore, &profile.ManuallyTrusted, &profile.ChallengeRequiredUntil, &profile.ReviewRequiredUntil,
			&profile.RestrictedUntil, &profile.RestrictionMode, &profile.RestrictionActions, &profile.RestrictionEnd)
		if err != nil {
			return nil, err
		}
		return json.Marshal(profile)
	})
	if err != nil {
		return accountProfile{}, err
	}
	var profile accountProfile
	if err = json.Unmarshal(raw, &profile); err != nil {
		return accountProfile{}, err
	}
	return profile, nil
}

func classifyTrust(profile accountProfile, settings Settings, now time.Time) string {
	if profile.ManuallyTrusted {
		return "trusted"
	}
	if profile.TrustLevel == "high_risk" || profile.TrustLevel == "restricted" {
		return profile.TrustLevel
	}
	age := now.Sub(profile.CreatedAt)
	if age < time.Duration(settings.NewAccountDays)*24*time.Hour || !profile.EmailVerified {
		return "new"
	}
	if age >= time.Duration(settings.TrustedAccountDays)*24*time.Hour && profile.Level >= settings.TrustedMinimumLevel {
		return "trusted"
	}
	return "normal"
}

func restrictionApplies(profile accountProfile, action string, now time.Time) bool {
	if profile.RestrictionMode == "challenge" || profile.RestrictionMode == "moderation" {
		return false
	}
	return restrictionMatchesAction(profile, action, now)
}

func restrictionMatchesAction(profile accountProfile, action string, now time.Time) bool {
	if profile.RestrictionMode == "" || !profile.RestrictionEnd.After(now) {
		return false
	}
	if profile.RestrictionMode == "read_only" || profile.RestrictionMode == "temporary_ban" || profile.RestrictionMode == "permanent_ban" {
		return true
	}
	for _, restricted := range profile.RestrictionActions {
		if restricted == action || restricted == "*" {
			return true
		}
	}
	return false
}

func (s *Service) pendingReviewCount(ctx context.Context, userID int64) (int, error) {
	var count int
	err := s.db.QueryRow(ctx, `select count(*) from change_requests where submitted_by=$1 and status='pending'`, userID).Scan(&count)
	return count, err
}

func (s *Service) privateHash(namespace, value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(s.cfg.IPHashSecret))
	_, _ = mac.Write([]byte(namespace + "\x00" + value))
	return hex.EncodeToString(mac.Sum(nil))
}

func subnetForIP(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}
	if four := ip.To4(); four != nil {
		return fmt.Sprintf("%d.%d.%d.0/24", four[0], four[1], four[2])
	}
	bytes := ip.To16()
	if bytes == nil {
		return ""
	}
	for index := 7; index < len(bytes); index++ {
		bytes[index] = 0
	}
	return (&net.IPNet{IP: bytes, Mask: net.CIDRMask(56, 128)}).String()
}

func contentAction(action string) bool {
	return strings.HasPrefix(action, "comment.") || action == "message.send" || strings.Contains(action, "submit") || action == "report.create"
}

func suspiciousUserAgent(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "" || value == "curl" || strings.HasPrefix(value, "python-requests") || strings.HasPrefix(value, "go-http-client")
}

func contentHash(value string) string {
	normalized := NormalizeContent(value)
	if normalized == "" {
		return ""
	}
	return ContentHash(normalized)
}

func compactStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func randomToken(bytesCount int) (string, error) {
	value := make([]byte, bytesCount)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func tokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func nullableID(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}
func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
func bounded(value, minimum, maximum, fallback int) int {
	if value < minimum || value > maximum {
		return fallback
	}
	return value
}
func boundedAllowZero(value, maximum int) int {
	if value < 0 || value > maximum {
		return 0
	}
	return value
}

func (s *Service) cleanup(ctx context.Context) {
	_, _ = s.db.Exec(ctx, `delete from anti_abuse_challenges where id in (select id from anti_abuse_challenges where expires_at<now()-interval '1 day' order by id limit 1000)`)
	_, _ = s.db.Exec(ctx, `delete from anti_abuse_content_fingerprints where id in (select id from anti_abuse_content_fingerprints where created_at<now()-make_interval(days=>$1::int) order by id limit 1000)`, s.cfg.FingerprintRetentionDays)
	_, _ = s.db.Exec(ctx, `delete from anti_abuse_events where id in (select id from anti_abuse_events where created_at<now()-make_interval(days=>$1::int) order by id limit 1000)`, s.cfg.EventRetentionDays)
}
