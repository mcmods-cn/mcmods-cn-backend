package httpapi

import (
	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
	"net/http"
	"testing"
)

func auditFixGovernanceActivityFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := auditAccessActivityFacts(t, f)
	for _, table := range []string{"creator_team_members", "permission_audit_logs", "project_editor_applications", "project_editor_application_attachments", "project_editor_assignments", "user_permissions", "user_role_bindings"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}
func auditFixGovernanceFailThenRecover(t *testing.T, f test013Fixture, actor, method, path string, body any, action, object int16) {
	t.Helper()
	prior := auditFixActivityIntentCount(t, f, actor, action, object)
	auditFixInjectActivityFailure(t, f, f.userIDs[actor], true)
	before := auditFixGovernanceActivityFacts(t, f)
	raw := f.require(t, actor, method, path, body, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist governance activity")
	auditFixRequireProjectFactsUnchanged(t, before, auditFixGovernanceActivityFacts(t, f))
	auditFixInjectActivityFailure(t, f, f.userIDs[actor], false)
	f.require(t, actor, method, path, body, 200)
	if count := auditFixActivityIntentCount(t, f, actor, action, object); count != prior+1 {
		t.Fatalf("governance intents=%d prior=%d", count, prior)
	}
}
func TestAuditFixCMP003CreatorDecisionRevocationActivitySharesPermissionTransaction(t *testing.T) {
	for _, status := range []string{"approved", "rejected"} {
		t.Run(status, func(t *testing.T) {
			f := newTEST013Fixture(t)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "creator.claim")
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "creator.claim.review")
			var creator string
			if err := f.db.QueryRow(f.ctx, `insert into creators(kind,name,normalized_name,review_status) values('author','Atomic decision author','atomic decision author','approved') returning public_id`).Scan(&creator); err != nil {
				t.Fatal(err)
			}
			claim := decodeTEST022Data[struct {
				ID string `json:"id"`
			}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/creators/"+creator+"/claims", map[string]string{"proofMarkdown": "Owned independent author proof"}, 201))
			if _, err := f.db.Exec(f.ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,status,permission_granting) select 'mod',$1,id,'approved',true from creators where public_id=$2`, f.modID, creator); err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/admin/creator-claims/" + claim.ID
			auditFixGovernanceFailThenRecover(t, f, f.reviewer, http.MethodPatch, path, map[string]string{"status": status, "note": "Owned independent review"}, activity.ActionEdit, activity.ObjectReview)
			var derived bool
			if err := f.db.QueryRow(f.ctx, `select exists(select 1 from effective_project_access where user_id=$1 and project_id=$2 and source_type='author_claim')`, f.userIDs[f.editor], f.modID).Scan(&derived); err != nil || derived != (status == "approved") {
				t.Fatalf("review effective access=%v status=%s err=%v", derived, status, err)
			}
			if status == "approved" {
				auditFixGovernanceFailThenRecover(t, f, f.reviewer, http.MethodDelete, path, map[string]string{"reason": "Owned withdrawal fixture"}, activity.ActionDelete, activity.ObjectReview)
				if err := f.db.QueryRow(f.ctx, `select exists(select 1 from effective_project_access where user_id=$1 and project_id=$2 and source_type='author_claim')`, f.userIDs[f.editor], f.modID).Scan(&derived); err != nil || derived {
					t.Fatalf("revocation access=%v err=%v", derived, err)
				}
			}
		})
	}
}
func TestAuditFixCMP003TeamMembershipActivitySharesAuditTransaction(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "team.members.manage")
	var team, author, role string
	for _, row := range []struct {
		kind, name string
		out        *string
	}{{"team", "Atomic team", &team}, {"author", "Atomic member", &author}} {
		if err := f.db.QueryRow(f.ctx, `insert into creators(kind,name,normalized_name,review_status) values($1,$2,lower($2),'approved') returning public_id`, row.kind, row.name).Scan(row.out); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.QueryRow(f.ctx, `select public_id from creator_role_definitions order by id limit 1`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"members": []creatorMemberPayload{{CreatorID: author, RoleID: role, Title: "Owned member"}}}
	auditFixGovernanceFailThenRecover(t, f, f.editor, http.MethodPut, "/api/v1/creators/"+team+"/members", body, activity.ActionEdit, activity.ObjectAuthor)
	var count int
	if err := f.db.QueryRow(f.ctx, `select count(*) from creator_team_members member join creators team on team.id=member.team_id join creators author on author.id=member.member_creator_id where team.public_id=$1 and author.public_id=$2 and member.status='approved'`, team, author).Scan(&count); err != nil || count != 1 {
		t.Fatalf("actual member links=%d err=%v", count, err)
	}
}
func TestAuditFixCMP003EditorApplicationDecisionActivitySharesAssignmentTransaction(t *testing.T) {
	for _, status := range []string{"approved", "rejected"} {
		t.Run(status, func(t *testing.T) {
			f := newTEST013Fixture(t)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.editor.apply")
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "project.editor.review")
			app := decodeTEST022Data[projectEditorApplicationResponse](t, f.require(t, f.otherEditor, http.MethodPost, "/api/v1/projects/mod/"+f.modCode+"/editor-applications", map[string]string{"proofMarkdown": "Owned editor evidence"}, 201))
			auditFixGovernanceFailThenRecover(t, f, f.reviewer, http.MethodPatch, "/api/v1/admin/project-editor-applications/"+app.ID, map[string]string{"status": status, "note": "Owned independent decision"}, activity.ActionEdit, activity.ObjectReview)
			var count int
			if err := f.db.QueryRow(f.ctx, `select count(*) from project_editor_assignments where user_id=$1 and status='active'`, f.userIDs[f.otherEditor]).Scan(&count); err != nil || count != map[bool]int{true: 1, false: 0}[status == "approved"] {
				t.Fatalf("actual assignment count=%d status=%s err=%v", count, status, err)
			}
		})
	}
}
