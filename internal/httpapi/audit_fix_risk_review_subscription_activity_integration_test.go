package httpapi

import (
	"net/http"
	"testing"

	"mcmods-cn-backend/internal/activity"
)

func auditFixRiskStateFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := f.facts(t)
	var raw string
	if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from anti_abuse_user_states fact`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	facts["anti_abuse_user_states"] = raw
	return facts
}
func TestAuditFixCMP003AntiAbuseUserStateActivitySharesStateTransaction(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "security.anti-abuse.write")
	var target string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[f.editor]).Scan(&target); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/anti-abuse/users/" + target
	body := map[string]any{"trustLevel": "restricted", "riskScore": 800, "manuallyTrusted": false}
	auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], true)
	before := auditFixRiskStateFacts(t, f)
	raw := f.require(t, f.reviewer, http.MethodPatch, path, body, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist anti-abuse user state activity")
	auditFixRequireProjectFactsUnchanged(t, before, auditFixRiskStateFacts(t, f))
	auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], false)
	f.require(t, f.reviewer, http.MethodPatch, path, body, 200)
	var level string
	var risk int
	if err := f.db.QueryRow(f.ctx, `select trust_level,risk_score from anti_abuse_user_states where user_id=$1`, f.userIDs[f.editor]).Scan(&level, &risk); err != nil || level != "restricted" || risk != 800 {
		t.Fatalf("risk state=%s/%d err=%v", level, risk, err)
	}
	if got := auditFixActivityIntentCount(t, f, f.reviewer, activity.ActionEdit, activity.ObjectUser); got != 1 {
		t.Fatalf("risk change intents=%d", got)
	}
	for _, attempt := range []struct {
		actor, path string
		body        any
		status      int
	}{{f.editor, path, body, 403}, {f.reviewer, "/api/v1/admin/anti-abuse/users/absent001", body, 404}, {f.reviewer, path, map[string]any{"trustLevel": "restricted", "riskScore": -1}, 400}} {
		before = auditFixRiskStateFacts(t, f)
		f.require(t, attempt.actor, http.MethodPatch, attempt.path, attempt.body, attempt.status)
		auditFixRequireProjectFactsUnchanged(t, before, auditFixRiskStateFacts(t, f))
	}
}
func TestAuditFixCMP003ReviewSubscriptionActivityAndNotificationIntentAreAtomic(t *testing.T) {
	f := newTEST017Fixture(t)
	proposal := f.submit(t, f.editor, http.MethodPost, "/api/v1/changelogs?targetType=mod&targetId="+f.modCode, test017Snapshot("Subscription transaction fixture"), 201)
	path := "/api/v1/review-locks/project_changelog/" + proposal.ID + "/subscribe"
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
	before := f.facts(t)
	raw := f.require(t, f.editor, http.MethodPost, path, nil, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist review subscription activity")
	f.unchanged(t, before)
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
	f.require(t, f.editor, http.MethodPost, path, nil, 200)
	if got := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, activity.ActionCreate, activity.ObjectReview); got != 1 {
		t.Fatalf("first subscription intents=%d", got)
	}
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
	before = f.facts(t)
	f.require(t, f.editor, http.MethodPost, path, nil, 200)
	f.unchanged(t, before)
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
	before = f.facts(t)
	f.require(t, f.otherEditor, http.MethodPost, path, nil, 403)
	f.unchanged(t, before)
	f.review(t, f.denied, proposal, "approved", 200)
	var subscriptions, notified, notifications int
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from review_completion_subscriptions),(select count(*) from review_completion_subscriptions where notified_at is not null),(select count(*) from nats_outbox where event_type='review.completed')`).Scan(&subscriptions, &notified, &notifications); err != nil || subscriptions != 1 || notified != 1 || notifications != 1 {
		t.Fatalf("review subscription/notified/notification intents=%d/%d/%d err=%v", subscriptions, notified, notifications, err)
	}
	before = f.facts(t)
	f.require(t, f.editor, http.MethodPost, path, nil, 409)
	f.unchanged(t, before)
}
