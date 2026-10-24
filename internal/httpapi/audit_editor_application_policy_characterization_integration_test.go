package httpapi

import (
	"net/http"
	"testing"
)

// REM-Q02: characterize the editor-application gate as implemented. The
// project-reviews.md global/scoped self-review rule applies to content revisions;
// these endpoints currently require project.editor.review instead. This test
// records current behavior, without deciding a new separation-of-duties policy.
func TestAuditREM_Q02EditorApplicationActualSelfReviewGateIntegration(t *testing.T) {
	for _, kind := range []string{"ordinary-applicant", "global-editor-reviewer", "content-reviewer", "scoped-content-reviewer", "scoped-editor-reviewer", "other-project-editor-reviewer", "administrator"} {
		t.Run(kind, func(t *testing.T) {
			f := newTEST013Fixture(t)
			actor := f.otherEditor
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[actor], "project.editor.apply")
			app := decodeTEST022Data[projectEditorApplicationResponse](t, f.require(t, actor, http.MethodPost, "/api/v1/projects/mod/"+f.modCode+"/editor-applications", map[string]string{"proofMarkdown": "Synthetic current policy evidence"}, 201))
			permission := ""
			want := http.StatusForbidden
			switch kind {
			case "global-editor-reviewer":
				permission = "project.editor.review"
				want = http.StatusOK
			case "content-reviewer":
				permission = "content.review"
			case "scoped-content-reviewer":
				permission = "project.review." + f.modCode
			case "scoped-editor-reviewer":
				permission = "project.editor.review." + f.modCode
			case "other-project-editor-reviewer":
				permission = "project.editor.review." + f.otherModCode
			case "administrator":
				permission = "admin.*"
				want = http.StatusOK
			}
			if permission != "" {
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[actor], permission)
			}
			before := auditFixGovernanceActivityFacts(t, f)
			f.require(t, actor, http.MethodPatch, "/api/v1/admin/project-editor-applications/"+app.ID, map[string]string{"status": "approved", "note": "Current policy characterization"}, want)
			if want == http.StatusForbidden {
				auditFixRequireProjectFactsUnchanged(t, before, auditFixGovernanceActivityFacts(t, f))
				return
			}
			var status string
			var applicant, reviewer int64
			var assignments int
			if err := f.db.QueryRow(f.ctx, `select status,user_id,reviewed_by,(select count(*) from project_editor_assignments where user_id=application.user_id and target_route_id=application.target_route_id and status='active') from project_editor_applications application where public_id=$1`, app.ID).Scan(&status, &applicant, &reviewer, &assignments); err != nil {
				t.Fatal(err)
			}
			if status != "approved" || applicant != f.userIDs[actor] || reviewer != applicant || assignments != 1 {
				t.Fatalf("policy facts=%s applicant=%d reviewer=%d assignments=%d", status, applicant, reviewer, assignments)
			}
		})
	}
}
