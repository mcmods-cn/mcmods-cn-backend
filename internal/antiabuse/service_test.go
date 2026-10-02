package antiabuse

import (
	"context"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestPolicyUsesTrustRestrictionAndPermissionForElevation(t *testing.T) {
	settings := DefaultSettings()
	base := settings.Policies["comment.create"]
	newPolicy := policyFor(settings, "comment.create", "new", 100)
	trusted := policyFor(settings, "comment.create", "trusted", 100)
	if newPolicy.BurstLimit >= base.BurstLimit || trusted.BurstLimit != base.BurstLimit {
		t.Fatalf("unexpected differentiated limits: new=%+v base=%+v trusted=%+v", newPolicy, base, trusted)
	}
	elevated := policyFor(settings, "comment.create", "trusted", 200)
	if elevated.BurstLimit != base.BurstLimit*2 {
		t.Fatalf("trusted quota must be elevated by permission, got=%+v base=%+v", elevated, base)
	}
}

func TestPolicyUsesPermissionRateLimitPercent(t *testing.T) {
	settings := DefaultSettings()
	base := policyFor(settings, "review.submit", "normal", 100)
	elevated := policyFor(settings, "review.submit", "normal", 200)
	if elevated.BurstLimit != base.BurstLimit*2 || elevated.HourLimit != base.HourLimit*2 || elevated.DayLimit != base.DayLimit*2 || elevated.ObjectLimit != base.ObjectLimit*2 {
		t.Fatalf("permission percentage did not scale request quotas: base=%+v elevated=%+v", base, elevated)
	}
	if elevated.PendingLimit != base.PendingLimit {
		t.Fatalf("rate-limit permission must not expand the moderation queue: base=%d elevated=%d", base.PendingLimit, elevated.PendingLimit)
	}
	if got := NormalizeRateLimitPercent(0); got != DefaultRateLimitPercent {
		t.Fatalf("missing permission normalized to %d, want %d", got, DefaultRateLimitPercent)
	}
	if got := NormalizeRateLimitPercent(MaxRateLimitPercent + 1); got != MaxRateLimitPercent {
		t.Fatalf("oversized permission normalized to %d, want %d", got, MaxRateLimitPercent)
	}
}

func TestReviewSubmitRateScopesRemainReviewSubmit(t *testing.T) {
	create := rateLimitNamespace(Evaluation{Action: "review.submit", RateScope: "mod_content_version_create"})
	update := rateLimitNamespace(Evaluation{Action: "review.submit", RateScope: "mod_content_version_update"})
	other := rateLimitNamespace(Evaluation{Action: "review.submit"})
	if create != "review.submit:mod_content_version_create" || update != "review.submit:mod_content_version_update" || other != "review.submit" {
		t.Fatalf("unexpected review rate namespaces: create=%q update=%q other=%q", create, update, other)
	}
	if create == update || create == other {
		t.Fatal("content-version creation must not consume unrelated review-submit counters")
	}
}

func TestMapDecisionThresholds(t *testing.T) {
	settings := DefaultSettings()
	tests := []struct {
		score int
		want  Outcome
	}{{0, Allow}, {20, AllowWithLog}, {40, Moderation}, {60, Challenge}, {80, TempBlock}, {100, Deny}}
	for _, test := range tests {
		if got := mapDecision(test.score, []string{"test"}, settings).Outcome; got != test.want {
			t.Errorf("score %d mapped to %s, want %s", test.score, got, test.want)
		}
	}
}

func TestNormalizeSettingsRejectsUnsafeRanges(t *testing.T) {
	value := NormalizeSettings(Settings{LogThreshold: -1, ModerationThreshold: 1, ChallengeThreshold: 1, TempBlockThreshold: 1, DenyThreshold: 1,
		SimilarityThreshold: 1, Policies: map[string]ActionPolicy{"comment.create": {BurstLimit: -2}}})
	if !(value.LogThreshold <= value.ModerationThreshold && value.ModerationThreshold <= value.ChallengeThreshold && value.ChallengeThreshold <= value.TempBlockThreshold && value.TempBlockThreshold <= value.DenyThreshold) {
		t.Fatalf("threshold order is invalid: %+v", value)
	}
	if value.SimilarityThreshold < 700 || value.Policies["comment.create"].BurstLimit <= 0 {
		t.Fatalf("unsafe settings were not normalized: %+v", value)
	}
}

func TestValidateSettingsRejectsUnknownPolicyAndThresholdOrder(t *testing.T) {
	value := DefaultSettings()
	value.Policies["sql.from.client"] = ActionPolicy{BurstLimit: 1, BurstSeconds: 1, HourLimit: 1, DayLimit: 1, ObjectLimit: 1, ObjectMinutes: 1}
	if ValidateSettings(value) == nil {
		t.Fatal("unknown client policy was accepted")
	}
	value = DefaultSettings()
	value.ChallengeThreshold = value.LogThreshold - 1
	if ValidateSettings(value) == nil {
		t.Fatal("unordered thresholds were accepted")
	}
}

func TestValidateSettingsRejectsEverySilentlyNormalizedGlobalBound(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Settings)
	}{
		{"log threshold maximum", func(value *Settings) {
			value.LogThreshold, value.ModerationThreshold, value.ChallengeThreshold, value.TempBlockThreshold, value.DenyThreshold = 101, 101, 101, 101, 101
		}},
		{"moderation threshold maximum", func(value *Settings) {
			value.ModerationThreshold, value.ChallengeThreshold, value.TempBlockThreshold, value.DenyThreshold = 201, 201, 201, 201
		}},
		{"challenge threshold maximum", func(value *Settings) {
			value.ChallengeThreshold, value.TempBlockThreshold, value.DenyThreshold = 301, 301, 301
		}},
		{"temporary block threshold maximum", func(value *Settings) { value.TempBlockThreshold, value.DenyThreshold = 501, 501 }},
		{"trusted level minimum", func(value *Settings) { value.TrustedMinimumLevel = -1 }},
		{"trusted level maximum", func(value *Settings) { value.TrustedMinimumLevel = 1001 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := DefaultSettings()
			test.mutate(&value)
			if ValidateSettings(value) == nil {
				t.Fatalf("out-of-range settings were accepted: %+v", value)
			}
		})
	}
}

func TestTrustAndRestrictionClassification(t *testing.T) {
	now := time.Now()
	settings := DefaultSettings()
	if got := classifyTrust(accountProfile{CreatedAt: now.Add(-time.Hour), EmailVerified: true}, settings, now); got != "new" {
		t.Fatalf("new account classified as %q", got)
	}
	if got := classifyTrust(accountProfile{CreatedAt: now.Add(-365 * 24 * time.Hour), EmailVerified: true, Level: 10}, settings, now); got != "trusted" {
		t.Fatalf("established contributor classified as %q", got)
	}
	profile := accountProfile{RestrictionMode: "no_comment", RestrictionEnd: now.Add(time.Hour),
		RestrictionChallengeEnd: now.Add(time.Hour), RestrictionModerationEnd: now.Add(time.Hour)}
	if !restrictionApplies(profile, now) || !profile.RestrictionChallengeEnd.After(now) || !profile.RestrictionModerationEnd.After(now) {
		t.Fatal("action-scoped restriction effects were not retained")
	}
	profile.RestrictionMode = ""
	if restrictionApplies(profile, now) {
		t.Fatal("challenge and moderation effects must not become a hard block")
	}
}

func TestAccountRestrictionQueryFiltersBeforeAggregationAndScopesCacheByAction(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(strings.Join(strings.Fields(accountProfileSQL), " "))
	for _, required := range []string{
		"restriction.actions&&array[$4,'*']::text[]",
		"filter(where mode not in ('challenge','moderation'))",
		"filter(where mode='challenge')",
		"filter(where mode='moderation')",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("account restriction query is missing %q", required)
		}
	}
	if strings.Contains(definition, "limit 1") {
		t.Fatal("account restriction query truncates active restrictions before matching the action")
	}
	service := &Service{cfg: config.AntiAbuseConfig{IPHashSecret: "test-only"}}
	commentKey := service.accountCacheKey(42, "ip", "device", "comment.create")
	messageKey := service.accountCacheKey(42, "ip", "device", "message.send")
	if commentKey == messageKey || !strings.HasPrefix(commentKey, accountCacheKeyPrefix+"42:") {
		t.Fatalf("account cache is not action-scoped: comment=%q message=%q", commentKey, messageKey)
	}
}

func TestNetworkAndClientSignals(t *testing.T) {
	if got := subnetForIP("192.0.2.44"); got != "192.0.2.0/24" {
		t.Fatalf("IPv4 subnet = %q", got)
	}
	if !suspiciousUserAgent("python-requests/2.32") || suspiciousUserAgent("Mozilla/5.0 Firefox/141") {
		t.Fatal("client signal classification is incorrect")
	}
}

func TestAccountStateInvalidationOnlyRemovesTargetAccount(t *testing.T) {
	ctx := context.Background()
	cache := querycache.New(config.RedisConfig{})
	service := New(context.Background(), config.AntiAbuseConfig{}, nil, cache)
	targetPrefix := accountCacheKeyPrefix + "42:"
	targetKeys := []string{targetPrefix + "ip-a:device-a", targetPrefix + "ip-b:device-b"}
	otherKey := accountCacheKeyPrefix + "43:ip-a:device-a"
	for _, key := range append(targetKeys, otherKey) {
		cache.Set(ctx, key, []byte("cached"), time.Minute)
	}

	service.InvalidateAccountState(ctx, 42)
	for _, key := range targetKeys {
		if _, ok := cache.Get(ctx, key); ok {
			t.Fatalf("target account cache key %q was not invalidated", key)
		}
	}
	if _, ok := cache.Get(ctx, otherKey); !ok {
		t.Fatal("invalidating one account removed another account's cache")
	}
}
