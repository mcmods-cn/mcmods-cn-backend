package httpapi

import (
	"mcmods-cn-backend/internal/activity"
	"net/http"
	"testing"
)

func auditFixActivityIntentCount(t *testing.T, f test013Fixture, token string, action, object int16) int {
	t.Helper()
	var count int
	if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[token], action, object).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
func TestAuditFixCMP003BountySettlementActivitySharesFinancialTransaction(t *testing.T) {
	for _, operation := range []string{"answer", "self-solved"} {
		t.Run(operation, func(t *testing.T) {
			f := newTEST042Fixture(t)
			f.reviewPolicy(t, false)
			currency := f.seedMoney(t)
			snapshot := f.snapshot("discussion", "Atomic settlement "+operation)
			snapshot.BountyCurrency = "test042"
			snapshot.BountyAmount = 100
			item := f.create(t, f.editor, snapshot)
			path := "/api/v1/community/posts/" + item.ID + "/self-solved"
			if operation == "answer" {
				answer := f.comment(t, f.otherEditor, item.ID, "atomic-activity-"+operation)
				path = "/api/v1/community/posts/" + item.ID + "/answers/" + answer.ID
			}
			prior := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, activity.ActionCreate, activity.ObjectCommunityPost)
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
			before := f.facts(t)
			raw := f.require(t, f.editor, http.MethodPost, path, nil, 500)
			auditFixRequireSpecificActivityError(t, raw, "failed to persist settlement activity")
			f.unchanged(t, before)
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
			f.require(t, f.editor, http.MethodPost, path, nil, 200)
			current := f.detail(t, "", item.ID)
			if operation == "answer" {
				if current.Bounty.Status != "awarded" || current.ResolutionStatus != "answered" {
					t.Fatalf("actual recovered award=%+v", current.Bounty)
				}
				f.balance(t, f.editor, currency, 900)
				f.balance(t, f.otherEditor, currency, 1090)
			} else {
				if current.Bounty.Status != "refunded" || current.ResolutionStatus != "self_solved" {
					t.Fatalf("actual recovered refund=%+v", current.Bounty)
				}
				f.balance(t, f.editor, currency, 1000)
				f.balance(t, f.otherEditor, currency, 1000)
			}
			if count := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, activity.ActionCreate, activity.ObjectCommunityPost); count != prior+1 {
				t.Fatalf("settlement intents=%d prior=%d", count, prior)
			}
			before = f.facts(t)
			f.require(t, f.editor, http.MethodPost, path, nil, 409)
			f.unchanged(t, before)
		})
	}
}
func TestAuditFixCMP003ServerDecisionActivitySharesNotificationTransaction(t *testing.T) {
	for _, status := range []string{"approved", "rejected"} {
		t.Run(status, func(t *testing.T) {
			f := newTEST015ServerFixture(t)
			proof := f.proof(t, f.editor, "active", "clean", 100)
			_, id := f.create(t, f.editor, test015Submission("atomic-review.invalid:25565", proof))
			path := "/api/v1/admin/server-reviews/" + id
			body := map[string]string{"status": status, "note": "Owned independent decision fixture"}
			prior := auditFixActivityIntentCount(t, f.test013Fixture, f.reviewer, activity.ActionEdit, activity.ObjectReview)
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.reviewer], true)
			before := f.serverFacts(t)
			raw := f.require(t, f.reviewer, http.MethodPatch, path, body, 500)
			auditFixRequireSpecificActivityError(t, raw, "failed to persist settlement activity")
			f.serverUnchanged(t, before)
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.reviewer], false)
			f.require(t, f.reviewer, http.MethodPatch, path, body, 200)
			var actual string
			var notifications int
			if err := f.db.QueryRow(f.ctx, `select review_status,(select count(*) from nats_outbox where payload->'data'->>'serverId'=$1 and payload->>'templateKey'=$2) from minecraft_servers where public_id=$1`, id, "review_"+status).Scan(&actual, &notifications); err != nil || actual != status || notifications != 1 {
				t.Fatalf("decision status=%s intents=%d err=%v", actual, notifications, err)
			}
			if count := auditFixActivityIntentCount(t, f.test013Fixture, f.reviewer, activity.ActionEdit, activity.ObjectReview); count != prior+1 {
				t.Fatalf("decision activity=%d prior=%d", count, prior)
			}
			before = f.serverFacts(t)
			f.require(t, f.reviewer, http.MethodPatch, path, body, 409)
			f.serverUnchanged(t, before)
		})
	}
}
