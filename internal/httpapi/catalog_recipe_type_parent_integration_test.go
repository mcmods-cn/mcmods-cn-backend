package httpapi

import (
	"net/http"
	"testing"
)

func TestRecipeCollectionDistinguishesParentStateFromStorageFailureIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "content.write")
	var parent string
	if err := f.db.QueryRow(f.ctx, `insert into catalog_entities(identity_key,entity_type,status)
		values('audit:type-read-state','recipe_type','placeholder') returning public_id`).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	check := func(id string, want int) {
		t.Helper()
		prefix := "/api/v1/recipe-types/" + id
		f.require(t, "", http.MethodGet, prefix+"/templates", nil, want)
		f.require(t, "", http.MethodGet, prefix+"/recipes", nil, want)
		f.require(t, f.editor, http.MethodPost, prefix+"/templates", map[string]any{}, want)
		f.require(t, f.editor, http.MethodPost, prefix+"/recipes", map[string]any{}, want)
		f.require(t, f.editor, http.MethodPost, "/api/v1/catalog/recipes", map[string]any{"recipeTypePublicId": id}, want)
	}
	check(parent, http.StatusNotFound)
	check(randomCatalogPublicID(), http.StatusNotFound)
	// The fixture owns its full database. Temporarily take the relation offline
	// to distinguish a required read failure from a genuine missing parent.
	if _, err := f.db.Exec(f.ctx, `alter table catalog_entities rename to catalog_entities_offline`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := f.db.Exec(f.ctx, `alter table catalog_entities_offline rename to catalog_entities`); err != nil {
			t.Error(err)
		}
	}()
	check(parent, http.StatusInternalServerError)
}
