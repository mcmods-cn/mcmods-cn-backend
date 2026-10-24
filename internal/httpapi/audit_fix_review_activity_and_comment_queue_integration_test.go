package httpapi

import (
	"net/http"
	"net/url"
	"testing"

	"mcmods-cn-backend/internal/activity"
)

func TestAuditFixCMP003CatalogReviewActivitySharesDecisionTransaction(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "content.write")
	payload := map[string]any{"kindCode": "minecraft.item", "canonicalId": "audit:atomic-review", "defaultLocale": "en-US", "localizations": []map[string]string{{"locale": "en-US", "name": "Atomic review resource"}}, "definition": map[string]any{}, "reason": "owned atomic review fixture"}
	created := decodeTEST022Data[catalogEditResult](t, f.require(t, f.editor, http.MethodPost, "/api/v1/catalog/resources", payload, 202))
	path := "/api/v1/content-revisions/" + created.RevisionID
	auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], true)
	before := f.facts(t)
	raw := f.require(t, f.reviewer, http.MethodPatch, path, map[string]string{"status": "approved"}, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to record catalog review")
	f.unchanged(t, before)
	auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], false)
	f.require(t, f.reviewer, http.MethodPatch, path, map[string]string{"status": "approved"}, 200)
	f.require(t, f.reviewer, http.MethodPatch, path, map[string]string{"status": "approved"}, 409)
	var intents int
	if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and object_type_id=$2 and action_id=$3`, f.userIDs[f.reviewer], activity.ObjectReview, activity.ActionEdit).Scan(&intents); err != nil || intents != 1 {
		t.Fatalf("successful manual-review intents=%d want1 err=%v", intents, err)
	}
}

func TestAuditFixCMP004ModerationQueueKeysetSurvivesDecisionsAndInvisibleParents(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "comment.moderate")
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='rejected' where id=$1`, f.otherModID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	for i := 0; i < 110; i++ {
		target := commentTargetInfo{Type: "mod", Key: f.modCode, InternalID: f.modID}
		body := "Visible synthetic pending queue"
		if i < 5 {
			target.Key = f.otherModCode
			target.InternalID = f.otherModID
			body = "HIDDEN_PARENT_QUEUE_BODY"
		}
		inserted, insertErr := insertCommentTree(f.ctx, tx, target, f.userIDs[f.editor], createCommentRequest{Body: body, Status: "pending", IdempotencyKey: "queue-" + randomCatalogPublicID()})
		if insertErr != nil {
			t.Fatal(insertErr)
		}
		if i < 5 {
			if _, err = tx.Exec(f.ctx, `update comments set created_at=created_at-interval '1 hour' where id=$1`, inserted.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	type page struct {
		Items      []pendingCommentResponse `json:"items"`
		HasMore    bool                     `json:"hasMore"`
		NextCursor string                   `json:"nextCursor"`
	}
	first := decodeTEST022Data[page](t, f.require(t, f.reviewer, http.MethodGet, "/api/v1/admin/comments/pending?limit=20", nil, 200))
	if len(first.Items) != 15 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first raw20 must hide5 and retain cursor: items=%d more=%v", len(first.Items), first.HasMore)
	}
	seen := map[string]bool{}
	for _, item := range first.Items {
		if item.Body == "HIDDEN_PARENT_QUEUE_BODY" {
			t.Fatal("hidden parent comment leaked")
		}
		seen[item.ID] = true
	}
	// The cursor member itself changes status before reading the next page.
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/comments/"+first.Items[len(first.Items)-1].ID+"/review", map[string]string{"status": "approved"}, 200)
	f.require(t, f.reviewer, http.MethodGet, "/api/v1/admin/comments/pending?status=hidden&cursor="+url.QueryEscape(first.NextCursor), nil, 400)
	cursor := first.NextCursor
	for pages := 0; cursor != ""; pages++ {
		if pages > 10 {
			t.Fatal("moderation cursor did not advance")
		}
		value := decodeTEST022Data[page](t, f.require(t, f.reviewer, http.MethodGet, "/api/v1/admin/comments/pending?limit=20&cursor="+url.QueryEscape(cursor), nil, 200))
		for _, item := range value.Items {
			if seen[item.ID] || item.Body == "HIDDEN_PARENT_QUEUE_BODY" {
				t.Fatalf("queue duplicated or leaked item %s", item.ID)
			}
			seen[item.ID] = true
		}
		if value.HasMore && value.NextCursor == "" {
			t.Fatal("queue omitted continuation")
		}
		cursor = value.NextCursor
	}
	if len(seen) != 105 {
		t.Fatalf("moderation pagination lost pending comments: seen=%d want105", len(seen))
	}
}
