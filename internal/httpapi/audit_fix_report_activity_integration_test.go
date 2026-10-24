package httpapi

import (
	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
	"net/http"
	"testing"
)

func auditFixReportActivityFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := f.facts(t)
	for _, table := range []string{"reports", "report_snapshots", "report_assignment_events", "report_reviews", "report_evidence", "moderation_actions", "permission_audit_logs"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}
func TestAuditFixCMP003ReportOwnershipAndResolutionActivityShareWorkflowTransaction(t *testing.T) {
	for _, conclusion := range []string{"valid", "invalid"} {
		t.Run(conclusion, func(t *testing.T) {
			f := newTEST013Fixture(t)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "report.create")
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "report.review", "report.action.reopen")
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "report.review", "report.action.takeover", "report.action.delete")
			created := decodeTEST022Data[struct {
				ID string `json:"id"`
			}](t, f.require(t, f.otherEditor, http.MethodPost, "/api/v1/reports", createUnifiedReportRequest{TargetType: "mod", TargetID: f.modCode, ReasonCode: "other", CustomReason: "Owned synthetic workflow fixture", Detail: "No real private user data"}, 201))
			path := "/api/v1/admin/reports/" + created.ID
			recover := func(actor, operation string, body any, action int16) {
				t.Helper()
				prior := auditFixActivityIntentCount(t, f, actor, action, activity.ObjectReview)
				auditFixInjectActivityFailure(t, f, f.userIDs[actor], true)
				before := auditFixReportActivityFacts(t, f)
				response := f.require(t, actor, http.MethodPost, path+"/"+operation, body, 500)
				auditFixRequireSpecificActivityError(t, response, "failed to persist report activity")
				auditFixRequireProjectFactsUnchanged(t, before, auditFixReportActivityFacts(t, f))
				auditFixInjectActivityFailure(t, f, f.userIDs[actor], false)
				f.require(t, actor, http.MethodPost, path+"/"+operation, body, 200)
				if count := auditFixActivityIntentCount(t, f, actor, action, activity.ObjectReview); count != prior+1 {
					t.Fatalf("report %s activity=%d prior=%d", operation, count, prior)
				}
			}
			recover(f.reviewer, "claim", nil, activity.ActionClaim)
			recover(f.denied, "takeover", map[string]string{"reason": "Owned audited takeover"}, activity.ActionCreate)
			recover(f.denied, "resolve", resolveUnifiedReportRequest{Conclusion: conclusion, Note: "Owned independent decision", DeleteTarget: conclusion == "valid", IdempotencyKey: "audit-report-decision"}, activity.ActionCreate)
			var modState string
			if err := f.db.QueryRow(f.ctx, `select review_status from mods where id=$1`, f.modID).Scan(&modState); err != nil {
				t.Fatal(err)
			}
			if (conclusion == "invalid" && modState != "approved") || (conclusion == "valid" && modState == "approved") {
				t.Fatalf("actual %s decision target state=%s", conclusion, modState)
			}
			recover(f.reviewer, "reopen", map[string]string{"reason": "Owned new evidence", "idempotencyKey": "audit-report-reopen"}, activity.ActionCreate)
			var state string
			var reviews, assignments int
			if err := f.db.QueryRow(f.ctx, `select status,(select count(*) from report_reviews where report_id=r.id),(select count(*) from report_assignment_events where report_id=r.id) from reports r where public_id=$1`, created.ID).Scan(&state, &reviews, &assignments); err != nil || state != "pending" || reviews != 2 || assignments != 2 {
				t.Fatalf("report recovered state=%s reviews=%d assignments=%d err=%v", state, reviews, assignments, err)
			}
			before := auditFixReportActivityFacts(t, f)
			f.require(t, f.reviewer, http.MethodPost, path+"/reopen", map[string]string{"reason": "duplicate", "idempotencyKey": "audit-report-reopen"}, 409)
			auditFixRequireProjectFactsUnchanged(t, before, auditFixReportActivityFacts(t, f))
		})
	}
}
