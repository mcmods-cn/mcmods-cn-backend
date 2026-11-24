package httpapi

import (
	"net/http"
	"testing"
)

func TestAuditOSS001CreatorReviewLockUsesActualKindAndExactEditorHTTPPostgresIntegration(t *testing.T) {
	for _, kind := range []string{"author", "team"} {
		t.Run(kind, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			id := auditOSS001CreateReviewedCreator(t, f, kind)
			path := "/api/v1/review-locks/creator/" + id
			var registered string
			if err := f.db.QueryRow(f.ctx, `select route.entity_type from public_routes route join creators c on c.id=route.internal_id and c.public_id=route.public_id and c.kind=route.entity_type where c.public_id=$1`, id).Scan(&registered); err != nil || registered != kind {
				t.Fatal("must use actual kind route instead of seeding a creator alias", err)
			}
			initial := decodeTEST022Data[reviewLockResponse](t, f.require(t, f.editor, http.MethodGet, path, nil, 200))
			if initial.Locked {
				t.Fatal("reviewed creator incorrectly locked")
			}
			f.require(t, "", http.MethodGet, path, nil, 401)
			f.require(t, f.otherEditor, http.MethodGet, path, nil, 403)
			f.require(t, f.reviewer, http.MethodGet, path, nil, 403)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "project.review")
			f.require(t, f.denied, http.MethodGet, path, nil, 403)
			changed := decodeTEST022Data[struct {
				Revision string `json:"revisionId"`
				Request  string `json:"changeRequestId"`
				Status   string `json:"reviewStatus"`
			}](t, f.require(t, f.editor, http.MethodPut, "/api/v1/creators/"+id, map[string]any{"baseRevisionId": auditOSS001PublishedCreatorBody(t, f, id)["baseRevisionId"], "kind": kind, "name": "Edited profile for review lock " + id, "descriptionMarkdown": "A distinct requested biography", "defaultLocale": "en-US", "localizations": []map[string]any{{"locale": "en-US", "contentMarkdown": "A distinct requested biography"}}, "avatarUrl": "", "links": []any{}}, 200))
			if changed.Status != "pending" || changed.Revision == "" || changed.Request == "" {
				t.Fatal("ordinary edit must remain pending")
			}
			pending := decodeTEST022Data[reviewLockResponse](t, f.require(t, f.editor, http.MethodGet, path, nil, 200))
			if !pending.Locked || pending.RequestID != changed.Request || !pending.CanSubscribe {
				t.Fatal("logical creator must bind exact pending aggregate request")
			}
			f.require(t, f.editor, http.MethodPost, path+"/subscribe", map[string]any{}, 200)
			var subscriptions int
			if err := f.db.QueryRow(f.ctx, `select count(*) from review_completion_subscriptions sub join change_requests request on request.id=sub.change_request_id where request.public_id=$1`, changed.Request).Scan(&subscriptions); err != nil || subscriptions != 1 {
				t.Fatal("one actual creator subscription expected", err)
			}
			for _, actor := range []string{f.otherEditor, f.reviewer, f.denied} {
				f.require(t, actor, http.MethodPost, path+"/subscribe", map[string]any{}, 403)
			}
			var after int
			if err := f.db.QueryRow(f.ctx, `select count(*) from review_completion_subscriptions sub join change_requests request on request.id=sub.change_request_id where request.public_id=$1`, changed.Request).Scan(&after); err != nil || after != subscriptions {
				t.Fatal("denied subscription must have zero new rows", err)
			}
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+changed.Revision, map[string]any{"status": "approved", "note": "Independent completion clears creator lock"}, 200)
			final := decodeTEST022Data[reviewLockResponse](t, f.require(t, f.editor, http.MethodGet, path, nil, 200))
			if final.Locked {
				t.Fatal("resolved creator lock must clear")
			}
		})
	}
}
