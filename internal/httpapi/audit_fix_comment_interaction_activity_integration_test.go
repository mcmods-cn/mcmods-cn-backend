package httpapi

import (
	"mcmods-cn-backend/internal/activity"
	"net/http"
	"testing"
)

func TestAuditFixCMP003CommentPinAndReactionActivityShareInteractionTransaction(t *testing.T) {
	f := newTEST038Fixture(t)
	comment := decodeTEST022Data[commentResponse](t, f.require(t, f.otherEditor, http.MethodPost, "/api/v1/comment-targets/mod/"+f.modCode+"/comments", createCommentRequest{Body: "Owned comment interaction fixture", IdempotencyKey: "audit-interaction"}, 201))
	path := "/api/v1/comments/" + comment.ID
	for _, operation := range []struct {
		method, path string
		body         any
		action       int16
	}{
		{http.MethodPut, path + "/pin", nil, activity.ActionEdit},
		// Both pin verbs have always explicitly annotated Edit.
		{http.MethodDelete, path + "/pin", nil, activity.ActionEdit},
		{http.MethodPut, path + "/reaction", map[string]string{"reaction": "heart"}, activity.ActionEdit},
		{http.MethodDelete, path + "/reaction?reaction=heart", nil, activity.ActionDelete},
	} {
		prior := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, operation.action, activity.ObjectComment)
		auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
		before := f.facts(t)
		response := f.require(t, f.editor, operation.method, operation.path, operation.body, 500)
		auditFixRequireSpecificActivityError(t, response, "failed to persist comment interaction activity")
		f.unchanged(t, before)
		auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
		result := f.require(t, f.editor, operation.method, operation.path, operation.body, 200)
		if operation.path == path+"/pin" {
			actual := decodeTEST022Data[commentResponse](t, result)
			if actual.Pinned != (operation.method == http.MethodPut) {
				t.Fatalf("actual pin response=%v verb=%s", actual.Pinned, operation.method)
			}
		}
		if count := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, operation.action, activity.ObjectComment); count != prior+1 {
			t.Fatalf("interaction activity=%d prior=%d", count, prior)
		}
	}
	var reactions int
	var pinned bool
	if err := f.db.QueryRow(f.ctx, `select pinned_at is not null,(select count(*) from comment_reactions where comment_id=c.id) from comments c where public_id=$1`, comment.ID).Scan(&pinned, &reactions); err != nil || pinned || reactions != 0 {
		t.Fatalf("actual final pin=%v reactions=%d err=%v", pinned, reactions, err)
	}
}
