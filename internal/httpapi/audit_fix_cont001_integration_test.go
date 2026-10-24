package httpapi

import (
	"net/http"
	"testing"
)

// Parent fixtures use an explicit no-review role. Every tested child is
// submitted by an ordinary content.write user and reviewed by another user.
func TestAuditFixCONT001IndependentReviewFiveKinds(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "content.write")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "content.write", "content.no-review")
	base := func(name string) map[string]any {
		return map[string]any{"reason": "owned matrix", "defaultLocale": "en-US", "localizations": []map[string]any{{"locale": "en-US", "name": name}}}
	}
	typePayload := base("Parent type")
	typePayload["canonicalId"] = "audit:matrix-parent"
	typePayload["definition"] = map[string]any{}
	parent := decodeTEST022Data[catalogEditResult](t, f.require(t, f.otherEditor, http.MethodPost, "/api/v1/recipe-types", typePayload, 202))
	if parent.ReviewStatus != "approved" {
		t.Fatal("parent fixture must be an actual published object")
	}
	templatePayload := base("Parent template")
	templatePayload["templateKey"] = "parent-grid"
	templatePayload["canvas"] = map[string]any{"width": 80, "height": 40, "imageScale": 1}
	templatePayload["definition"] = map[string]any{}
	templatePayload["slots"] = []map[string]any{{"slotKey": "output", "role": "output", "ordinal": 0, "rect": map[string]any{"x": 10, "y": 10, "width": 16, "height": 16}}}
	template := decodeTEST022Data[catalogEditResult](t, f.require(t, f.otherEditor, http.MethodPost, "/api/v1/recipe-types/"+parent.ObjectPublicID+"/templates", templatePayload, 202))
	if template.ReviewStatus != "approved" {
		t.Fatal("template fixture must be an actual published object")
	}
	cases := []struct {
		kind, path string
		extra      map[string]any
	}{
		{"resource", "/api/v1/catalog/resources", map[string]any{"kindCode": "minecraft.item", "canonicalId": "audit:matrix-resource", "definition": map[string]any{}}},
		{"tag", "/api/v1/tags", map[string]any{"registry": "minecraft:item", "canonicalId": "audit:matrix-tag", "memberResourcePublicIds": []string{}}},
		{"recipe_type", "/api/v1/recipe-types", map[string]any{"canonicalId": "audit:matrix-type", "definition": map[string]any{}, "catalystResourcePublicIds": []string{}}},
		{"recipe_template", "/api/v1/recipe-types/" + parent.ObjectPublicID + "/templates", map[string]any{"templateKey": "matrix-grid", "canvas": templatePayload["canvas"], "slots": templatePayload["slots"], "definition": map[string]any{}}},
		{"recipe", "/api/v1/recipe-types/" + parent.ObjectPublicID + "/recipes", map[string]any{"canonicalSourceId": "audit:matrix-recipe", "templatePublicId": template.ObjectPublicID, "applicableVersionIds": []string{"1.21.1"}, "definition": map[string]any{}, "bindings": map[string]any{"output": map[string]any{"candidates": []map[string]any{{"rawResourceId": "audit:unresolved", "kindCode": "minecraft.item", "amount": 1}}}}}},
	}
	for _, c := range cases {
		for _, action := range []string{"approved", "rejected"} {
			t.Run(c.kind+"/"+action, func(t *testing.T) {
				payload := base("Audit pending " + c.kind)
				for k, v := range c.extra {
					payload[k] = v
				}
				for _, key := range []string{"canonicalId", "canonicalSourceId", "templateKey"} {
					if value, ok := payload[key].(string); ok {
						payload[key] = value + "-" + action
					}
				}
				created := decodeTEST022Data[catalogEditResult](t, f.require(t, f.editor, http.MethodPost, c.path, payload, 202))
				if created.ReviewStatus != "pending" {
					t.Fatal("ordinary user bypassed review")
				}
				f.require(t, f.editor, http.MethodPatch, "/api/v1/content-revisions/"+created.RevisionID, map[string]string{"status": action}, http.StatusForbidden)
				f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+created.RevisionID, map[string]string{"status": action, "note": "independent regression"}, http.StatusOK)
				var state, objectState string
				var published *int64
				if err := f.db.QueryRow(f.ctx, `select q.status,e.status,e.published_revision_id from change_requests q join catalog_entities e on e.public_id=q.aggregate_key where q.public_id=$1`, created.ChangeRequestID).Scan(&state, &objectState, &published); err != nil {
					t.Fatal(err)
				}
				if state != action || (action == "approved" && (objectState != "active" || published == nil)) || (action == "rejected" && published != nil) {
					t.Fatalf("action=%s request=%s object=%s published=%v", action, state, objectState, published)
				}
			})
		}
	}
}
