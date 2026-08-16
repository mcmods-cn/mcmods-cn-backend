package antiabuse

import (
	"testing"
	"time"
)

func TestPolicyUsesTrustWithoutBypassingLimits(t *testing.T) {
	settings := DefaultSettings()
	base := settings.Policies["comment.create"]
	newPolicy := policyFor(settings, "comment.create", "new")
	trusted := policyFor(settings, "comment.create", "trusted")
	if newPolicy.BurstLimit >= base.BurstLimit || trusted.BurstLimit <= base.BurstLimit {
		t.Fatalf("unexpected differentiated limits: new=%+v base=%+v trusted=%+v", newPolicy, base, trusted)
	}
	if trusted.BurstLimit <= 0 {
		t.Fatal("trusted users must remain rate limited")
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

func TestTrustAndRestrictionClassification(t *testing.T) {
	now := time.Now()
	settings := DefaultSettings()
	if got := classifyTrust(accountProfile{CreatedAt: now.Add(-time.Hour), EmailVerified: true}, settings, now); got != "new" {
		t.Fatalf("new account classified as %q", got)
	}
	if got := classifyTrust(accountProfile{CreatedAt: now.Add(-365 * 24 * time.Hour), EmailVerified: true, Level: 10}, settings, now); got != "trusted" {
		t.Fatalf("established contributor classified as %q", got)
	}
	profile := accountProfile{RestrictionMode: "no_comment", RestrictionActions: []string{"comment.create"}, RestrictionEnd: now.Add(time.Hour)}
	if !restrictionApplies(profile, "comment.create", now) || restrictionApplies(profile, "message.send", now) {
		t.Fatal("action-scoped restriction mapping is incorrect")
	}
	profile.RestrictionMode = "challenge"
	if restrictionApplies(profile, "comment.create", now) || !restrictionMatchesAction(profile, "comment.create", now) {
		t.Fatal("challenge restriction must challenge rather than hard-block")
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
