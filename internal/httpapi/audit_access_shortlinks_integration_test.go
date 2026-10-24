package httpapi

import (
	"fmt"
	"net/http"
	"testing"
)

func TestAuditSL001CurrentObjectAndRecipeParentVisibilityFullHTTPIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	for _, state := range []string{"approved", "pending", "rejected", "approved"} {
		if _, err := f.db.Exec(f.ctx, `update mods set review_status=$2 where id=$1`, f.modID, state); err != nil {
			t.Fatal(err)
		}
		want := 200
		if state != "approved" {
			want = 404
		}
		for _, token := range []string{"", f.editor, f.reviewer} {
			f.require(t, token, http.MethodGet, "/api/v1/public-links/"+f.modCode, nil, want)
		}
	}
	postID, postPublicID, _ := createTEST043ApprovedCommunityPost(t, f.ctx, f.db, f.userIDs[f.editor])
	f.require(t, "", http.MethodGet, "/api/v1/public-links/"+postPublicID, nil, 200)
	if _, err := f.db.Exec(f.ctx, `update community_posts set status='deleted' where id=$1`, postID); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", f.editor, f.reviewer} {
		f.require(t, token, http.MethodGet, "/api/v1/public-links/"+postPublicID, nil, 404)
	}
	var typeID, templateID, recipeID int64
	publicIDs := map[string]string{}
	for _, entity := range []struct {
		kind string
		id   *int64
	}{{"recipe_type", &typeID}, {"recipe_template", &templateID}, {"recipe", &recipeID}} {
		var publicID string
		if err := f.db.QueryRow(f.ctx, `insert into catalog_entities(identity_key,entity_type,status,default_locale) values($1,$2,'active','en-US') returning id,public_id`, "audit-shortlink:"+entity.kind, entity.kind).Scan(entity.id, &publicID); err != nil {
			t.Fatal(err)
		}
		publicIDs[entity.kind] = publicID
	}
	for _, query := range []string{
		fmt.Sprintf(`insert into recipe_types(entity_id,canonical_id) values(%d,'audit:parent')`, typeID),
		fmt.Sprintf(`insert into recipe_layout_templates(entity_id,recipe_type_id,template_key,canvas_width,canvas_height) values(%d,%d,'audit-grid',32,32)`, templateID, typeID),
		fmt.Sprintf(`insert into recipes(entity_id,recipe_type_id,semantic_fingerprint,identity_source) values(%d,%d,'audit-shortlink','manual')`, recipeID, typeID),
		fmt.Sprintf(`insert into recipe_definitions(recipe_id,template_id,definition) values(%d,%d,'{}')`, recipeID, templateID),
	} {
		if _, err := f.db.Exec(f.ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{"/api/v1/catalog/recipes/" + publicIDs["recipe"], "/api/v1/public-links/" + publicIDs["recipe"], "/api/v1/recipe-templates/" + publicIDs["recipe_template"]}
	for _, path := range paths {
		f.require(t, "", http.MethodGet, path, nil, 200)
	}
	if _, err := f.db.Exec(f.ctx, `update catalog_entities set status='archived',archived_at=now() where id=$1`, typeID); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		f.require(t, "", http.MethodGet, path, nil, 404)
	}
}
