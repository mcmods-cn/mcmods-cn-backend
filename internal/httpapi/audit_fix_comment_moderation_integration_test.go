package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestAuditFixCMP004PendingCommentReviewRecoveryAndAtomicDelivery(t *testing.T) {
	f := newTEST013Fixture(t)
	s := f.origin.Config.Handler.(*Server)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "comment.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "comment.moderate")
	_, rules, err := s.resolveUserRootPermissionsUncached(f.ctx, f.userIDs[f.editor])
	if err != nil {
		t.Fatal(err)
	}
	create := func(key string, parent ...string) string {
		payload := map[string]any{"body": "ordinary synthetic comment", "idempotencyKey": key}
		if len(parent) > 0 {
			payload["parentId"] = parent[0]
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/comment-targets/mod/"+f.modCode+"/comments", bytes.NewReader(catalogJSON(payload)))
		request.SetPathValue("targetType", "mod")
		request.SetPathValue("targetKey", f.modCode)
		ctx := context.WithValue(f.ctx, claimsContextKey, security.Claims{Subject: f.userIDs[f.editor], PermissionRules: rules})
		request = request.WithContext(context.WithValue(ctx, antiAbuseModerationContextKey, true))
		recorder := httptest.NewRecorder()
		s.commentsForTarget(recorder, request)
		if recorder.Code != 202 {
			t.Fatalf("controlled moderation decision create=%d %s", recorder.Code, recorder.Body.String())
		}
		var result struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Data.ID
	}
	first := create("audit-comment-approve")
	var before int
	if err = f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1`, f.userIDs[f.editor]).Scan(&before); err != nil || before != 0 {
		t.Fatalf("pending rewarded prematurely count=%d err=%v", before, err)
	}
	review := func(token, id, status string, want int) {
		f.require(t, token, http.MethodPatch, "/api/v1/comments/"+id+"/review", map[string]string{"status": status, "note": "audit independent moderation"}, want)
	}
	review(f.editor, first, "approved", 403)
	review(f.denied, first, "approved", 403)
	f.require(t, f.reviewer, http.MethodGet, "/api/v1/admin/comments/pending", nil, 200)
	if _, err = f.db.Exec(f.ctx, `create function audit_reject_comment_activity() returns trigger language plpgsql as $$
		begin raise exception 'owned synthetic activity failure'; end $$;
		create trigger audit_reject_comment_activity before insert on activity_event_outbox
		for each row execute function audit_reject_comment_activity()`); err != nil {
		t.Fatal(err)
	}
	failure := f.require(t, f.reviewer, http.MethodPatch, "/api/v1/comments/"+first+"/review", map[string]string{"status": "approved"}, 500)
	if !bytes.Contains(failure, []byte("failed to persist comment activity")) {
		t.Fatalf("unrelated error satisfied atomicity test: %s", failure)
	}
	var afterFailure string
	var failedAudit int
	if err = f.db.QueryRow(f.ctx, `select status,(select count(*) from audit_events where aggregate_key=$1) from comments where public_id=$1`, first).Scan(&afterFailure, &failedAudit); err != nil || afterFailure != "pending" || failedAudit != 0 {
		t.Fatalf("failed activity partially published comment state=%s audit=%d err=%v", afterFailure, failedAudit, err)
	}
	if _, err = f.db.Exec(f.ctx, `drop trigger audit_reject_comment_activity on activity_event_outbox; drop function audit_reject_comment_activity()`); err != nil {
		t.Fatal(err)
	}
	review(f.reviewer, first, "approved", 200)
	review(f.reviewer, first, "approved", 409)
	var approved, activities, audits int
	if err = f.db.QueryRow(f.ctx, `select (select count(*) from comments where public_id=$1 and status='published'),
 (select count(*) from activity_event_outbox where object_public_id=$1 and user_id=$2),
 (select count(*) from audit_events where aggregate_key=$1 and action='comment.review.approved')`, first, f.userIDs[f.editor]).Scan(&approved, &activities, &audits); err != nil {
		t.Fatal(err)
	}
	if approved != 1 || activities != 1 || audits != 1 {
		t.Fatalf("publication/activity/audit=%d/%d/%d", approved, activities, audits)
	}
	second := create("audit-comment-reject")
	review(f.reviewer, second, "rejected", 200)
	review(f.reviewer, second, "approved", 409)
	hidden := f.require(t, f.reviewer, http.MethodGet, "/api/v1/admin/comments/pending?status=hidden", nil, 200)
	if !bytes.Contains(hidden, []byte(second)) {
		t.Fatal("rejected comment has no recovery listing")
	}
	review(f.reviewer, second, "restore", 200)
	review(f.reviewer, second, "approved", 200)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "comment.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "comment.watch")
	rootRaw := f.require(t, f.otherEditor, http.MethodPost, "/api/v1/comment-targets/mod/"+f.modCode+"/comments", map[string]any{"body": "Root for pending reply", "idempotencyKey": "root-pending-reply"}, 201)
	var root struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err = json.Unmarshal(rootRaw, &root); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.denied, http.MethodPut, "/api/v1/comments/"+root.Data.ID+"/watch", nil, 200)
	reply := create("audit-pending-reply", root.Data.ID)
	var unread, deliveries int
	if err = f.db.QueryRow(f.ctx, `select unread_count from comment_watches where user_id=$1`, f.userIDs[f.denied]).Scan(&unread); err != nil || unread != 0 {
		t.Fatalf("pending reply leaked watch count: %d %v", unread, err)
	}
	review(f.reviewer, reply, "approved", 200)
	review(f.reviewer, reply, "approved", 409)
	if err = f.db.QueryRow(f.ctx, `select (select unread_count from comment_watches where user_id=$1),
		(select count(*) from nats_outbox where payload->'data'->>'commentId'=$2)`, f.userIDs[f.denied], reply).Scan(&unread, &deliveries); err != nil || unread != 1 || deliveries != 2 {
		t.Fatalf("approved reply watch=%d deliveries=%d expected 1/2 err=%v", unread, deliveries, err)
	}
	third := create("audit-comment-hidden-parent")
	if _, err = f.db.Exec(f.ctx, `update mods set review_status='rejected' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	review(f.reviewer, third, "approved", 404)
	var state string
	if err = f.db.QueryRow(f.ctx, `select status from comments where public_id=$1`, third).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("hidden target changed comment %s %v", state, err)
	}
	t.Log(fmt.Sprintf("controlled anti-abuse decision + real creation handler/PG; HTTP independent approve/reject/restore, denied and repeated reviews, author activity exactly once"))
}

func TestPendingCommentReviewUsesCurrentProjectGrant(t *testing.T) {
	f := newTEST013Fixture(t)
	s := f.origin.Config.Handler.(*Server)
	var commentID string
	if err := f.db.QueryRow(f.ctx, `insert into comments(target_type,target_id,author_id,body,status)
		values('mod',$1,$2,'Synthetic pending comment','pending') returning public_id`, f.modID, f.userIDs[f.editor]).Scan(&commentID); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/comments/"+commentID+"/review", bytes.NewBufferString(`{"status":"rejected"}`)).WithContext(f.ctx)
	request.SetPathValue("commentId", commentID)
	request.Header.Set("Authorization", "Bearer "+f.denied)
	recorder := httptest.NewRecorder()
	scope := "project.comment.moderate." + f.modCode
	s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if claimsAllow(currentClaims(r), scope) {
			t.Fatal("fixture unexpectedly starts with project moderation permission")
		}
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], scope)
		s.reviewPendingComment(w, r)
	})(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("current project moderator review = %d %s", recorder.Code, recorder.Body.String())
	}
	var state string
	var reviewer int64
	if err := f.db.QueryRow(f.ctx, `select comment.status,audit.actor_id from comments comment
		join audit_events audit on audit.aggregate_type='comment_moderation' and audit.aggregate_key=comment.public_id
		and audit.action='comment.review.rejected' where comment.public_id=$1`, commentID).Scan(&state, &reviewer); err != nil {
		t.Fatal(err)
	}
	if state != "hidden" || reviewer != f.userIDs[f.denied] {
		t.Fatalf("review state=%s actor=%d", state, reviewer)
	}
}
