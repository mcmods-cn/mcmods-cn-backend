package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"errors"
	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

func TestOCT02PublicCatalogOmitsUnpublishedImportSourcesIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", "Source version")
	section := f.section(t, version.PublicID, f.builtin(t, "advancement"))
	resource := f.resource(t, version.PublicID, section.PublicID, "test013:catalog_secret", "minecraft.advancement", "advancement", map[string]any{})
	f.review(t, resource, f.editor, "approved")
	revisionID := oct02CInsertPublishedExportRevision(t, f, version.PublicID)
	var resourceID, typeID, tagID, templateID, recipeID int64
	if err := f.db.QueryRow(f.ctx, `select id from catalog_entities where public_id=$1`, resource.PublicID).Scan(&resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into resource_import_snapshots(id,resource_id,revision_id,registry,names,icon_path)
	 values($1,$2,$3,'advancements','{"en-US":"Secret unpublished translation"}','secret.png')`, newExportID(), resourceID, revisionID); err != nil {
		t.Fatal(err)
	}
	newEntity := func(kind string, id *int64) string {
		t.Helper()
		var publicID string
		if err := f.db.QueryRow(f.ctx, `insert into catalog_entities(identity_key,entity_type,status)
		 values($1,$2,'active') returning id,public_id`, "oct02-public-source:"+kind, kind).Scan(id, &publicID); err != nil {
			t.Fatal(err)
		}
		return publicID
	}
	typePublicID := newEntity("recipe_type", &typeID)
	tagPublicID := newEntity("tag", &tagID)
	templatePublicID := newEntity("recipe_template", &templateID)
	recipePublicID := newEntity("recipe", &recipeID)
	typeSnapshot, templateSnapshot := newExportID(), newExportID()
	for _, operation := range []struct {
		query string
		args  []any
	}{
		{`insert into recipe_types(entity_id,canonical_id) values($1,'oct02:private_type')`, []any{typeID}},
		{`insert into recipe_type_import_snapshots(id,recipe_type_id,revision_id,title_names,catalysts)
		 values($1,$2,$3,'{"en-US":"Secret private category"}','[]')`, []any{typeSnapshot, typeID, revisionID}},
		{`insert into catalog_tags(entity_id,registry,canonical_id) values($1,'minecraft:item','oct02:private_tag')`, []any{tagID}},
		{`insert into tag_import_snapshots(id,tag_id,revision_id) values($1,$2,$3)`, []any{newExportID(), tagID, revisionID}},
		{`insert into recipe_template_import_snapshots(id,recipe_type_snapshot_id,revision_id,recipe_type_id,canonical_template_id,
		 source_template_id,schema_version,template_collection_path,background_path) values($1,$2,$3,$4,$5,'private-grid','v1','templates.json','secret.png')`, []any{templateSnapshot, typeSnapshot, revisionID, typeID, templateID}},
		{`insert into recipe_layout_templates(entity_id,recipe_type_id,template_key,import_snapshot_id,canvas_width,canvas_height)
		 values($1,$2,'private-grid',$3,16,16)`, []any{templateID, typeID, templateSnapshot}},
		{`insert into recipes(entity_id,recipe_type_id,canonical_source_id,semantic_fingerprint,owner_mod_id,identity_source)
		 values($1,$2,'oct02:private_recipe','synthetic',$3,'generated_index')`, []any{recipeID, typeID, f.modID}},
		{`insert into recipe_import_snapshots(id,recipe_id,revision_id,source_recipe_id,source_id_kind,source_recipe_key,recipe_collection_path)
		 values($1,$2,$3,'oct02:private_recipe','generated_index','synthetic','recipes.json')`, []any{newExportID(), recipeID, revisionID}},
	} {
		if _, err := f.db.Exec(f.ctx, operation.query, operation.args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, subject := range []struct {
		id   int64
		kind string
	}{{typeID, "recipe_type"}, {tagID, "tag"}, {templateID, "recipe_template"}, {recipeID, "recipe"}} {
		if _, err := f.db.Exec(f.ctx, `insert into content_subjects(subject_type,subject_id,default_locale) values($1,$2,'en-US') on conflict(subject_type,subject_id) do nothing`, subject.kind, subject.id); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec(f.ctx, `insert into content_localizations(subject_type,subject_id,catalog_entity_id,locale,name,provenance,review_status) values($1,$2,$2,'en-US','Synthetic private source','import','approved') on conflict(subject_type,subject_id,locale) do update set name=excluded.name`, subject.kind, subject.id); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{
		"/api/v1/catalog/resource-presentation?ref=" + resource.PublicID,
		"/api/v1/tags/" + tagPublicID,
		"/api/v1/recipe-types/" + typePublicID,
		"/api/v1/recipe-types/" + typePublicID + "/catalog",
		"/api/v1/recipe-types/" + typePublicID + "/templates",
		"/api/v1/recipe-types/" + typePublicID + "/recipes",
		"/api/v1/recipe-templates/" + templatePublicID,
		"/api/v1/catalog/recipes/" + recipePublicID,
		"/api/v1/public-links/" + resource.PublicID,
		"/api/v1/public-links/" + tagPublicID,
		"/api/v1/public-links/" + typePublicID,
		"/api/v1/content/" + typePublicID,
		"/api/v1/content/" + tagPublicID,
		"/api/v1/content/" + templatePublicID,
		"/api/v1/content/" + recipePublicID,
	}
	for _, path := range paths {
		f.require(t, "", http.MethodGet, path, nil, http.StatusOK)
	}
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='pending' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `update catalog_dataset_state set version=version+1 where singleton`); err != nil {
		t.Fatal(err)
	}
	// Global catalog responses remain public-only even for an authenticated
	// project editor; private preview stays on the existing project endpoints.
	for _, path := range paths {
		for _, reader := range []struct{ label, token string }{{"visitor", ""}, {"editor", f.editor}} {
			t.Run(path+" actor="+reader.label, func(t *testing.T) {
				f.require(t, reader.token, http.MethodGet, path, nil, http.StatusNotFound)
			})
		}
	}

	for kind, id := range map[string]string{"tag": tagPublicID, "recipe_type": typePublicID} {
		for _, actorID := range []int64{0, f.userIDs[f.editor]} {
			if _, err := (&Server{db: f.db}).resolveCommentTarget(f.ctx, kind, id, security.Claims{Subject: actorID}); !errors.Is(err, pgx.ErrNoRows) {
				t.Errorf("private catalog comment target %s actor=%d err=%v wantNoRows", kind, actorID, err)
			}
		}
	}
	for _, id := range []string{typePublicID, tagPublicID, templatePublicID, recipePublicID} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/content/"+id+"/translations", strings.NewReader(`{"targetLocale":"en-US"}`))
		r.SetPathValue("publicId", id)
		r = r.WithContext(context.WithValue(f.ctx, claimsContextKey, security.Claims{Subject: f.userIDs[f.editor]}))
		w := httptest.NewRecorder()
		(&Server{db: f.db}).requestCatalogContentTranslation(w, r)
		if w.Code != http.StatusNotFound {
			t.Errorf("unpublished localization translation status=%d want404 body=%s", w.Code, w.Body.String())
		}
	}
	for _, path := range []string{
		"/api/v1/catalog/resources/" + resource.PublicID + "/icon",
		"/api/v1/catalog/resources/" + resource.PublicID + "/icon-small?version=" + version.PublicID,
		"/api/v1/catalog/resources/" + resource.PublicID + "/render",
		"/api/v1/recipe-templates/" + templatePublicID + "/background",
		"/api/v1/catalog/recipes/" + recipePublicID + "/render",
	} {
		f.require(t, f.editor, http.MethodGet, path, nil, http.StatusNotFound)
	}
	for _, path := range []string{"/api/v1/recipe-types?q=oct02:private_type", "/api/v1/tags?q=oct02:private_tag"} {
		raw := f.require(t, f.editor, http.MethodGet, path, nil, http.StatusOK)
		var response struct {
			Data struct {
				Total int `json:"total"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &response); err != nil || response.Data.Total != 0 {
			t.Errorf("private identity leaked into public count: %s err=%v", raw, err)
		}
	}
	raw := f.require(t, f.editor, http.MethodPost, "/api/v1/catalog/resource-presentations", catalogPresentationBatchRequest{
		Locale: "en-US", Items: []catalogPresentationBatchReference{{PublicID: resource.PublicID}, {PublicID: tagPublicID, Kind: "tag"}},
	}, http.StatusOK)
	if strings.Contains(string(raw), resource.PublicID) || strings.Contains(string(raw), tagPublicID) || strings.Contains(string(raw), "Secret") {
		t.Fatalf("private presentation leaked: %s", raw)
	}
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='approved' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `update catalog_dataset_state set version=version+1 where singleton`); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		f.require(t, "", http.MethodGet, path, nil, http.StatusOK)
	}
	// Exercise the supported publication transaction after warming the shared
	// catalog response. This deliberately does not bump the generation in test SQL.
	listPath := "/api/v1/recipe-types?q=oct02:private_type"
	assertTotal := func(want int) {
		t.Helper()
		raw := f.require(t, "", http.MethodGet, listPath, nil, http.StatusOK)
		var response struct {
			Data struct {
				Total int `json:"total"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &response); err != nil || response.Data.Total != want {
			t.Fatalf("public generation total=%d want=%d err=%v body=%s", response.Data.Total, want, err, raw)
		}
	}
	assertTotal(1)
	assertTotal(1)
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if err := (&Server{db: f.db}).moderationHideTarget(f.ctx, tx, "mod", f.modCode); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	assertTotal(0)
	f.require(t, "", http.MethodGet, "/api/v1/recipe-types/"+typePublicID, nil, http.StatusNotFound)
}

func TestOCT02CommunityResourceReferencesRevalidatePublishedSourcesIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", "Community referenced version")
	section := f.section(t, version.PublicID, f.builtin(t, "advancement"))
	resource := f.resource(t, version.PublicID, section.PublicID, "test013:community_secret", "minecraft.advancement", "advancement", map[string]any{})
	f.review(t, resource, f.editor, "approved")
	revisionID := oct02CInsertPublishedExportRevision(t, f, version.PublicID)
	var resourceID, postID int64
	if err := f.db.QueryRow(f.ctx, `select id from catalog_entities where public_id=$1`, resource.PublicID).Scan(&resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into resource_import_snapshots(id,resource_id,revision_id,registry,names,icon_path)
	 values($1,$2,$3,'advancements','{"en-US":"Private community source"}','private.png')`, newExportID(), resourceID, revisionID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into community_posts(kind,category,author_id,title,source_locale,review_status)
	 values('news','other',$1,'Synthetic references','en-US','approved') returning id`, f.userIDs[f.editor]).Scan(&postID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into community_post_resource_refs(post_id,resource_id,kind_code) values($1,$2,'minecraft.advancement')`, postID, resourceID); err != nil {
		t.Fatal(err)
	}
	refs, err := loadCommunityPostResourceReferencesWithQueryer(f.ctx, f.db, []int64{postID})
	if err != nil || len(refs[postID]) != 1 || refs[postID][0].PublicID != resource.PublicID {
		t.Fatalf("published reference missing refs=%+v err=%v", refs[postID], err)
	}
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='pending' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	refs, err = loadCommunityPostResourceReferencesWithQueryer(f.ctx, f.db, []int64{postID})
	if err != nil || len(refs[postID]) != 1 {
		t.Fatalf("reference read failed refs=%+v err=%v", refs[postID], err)
	}
	ref := refs[postID][0]
	if !ref.Unavailable || ref.Unresolved || ref.PublicID != "" || ref.Name != "" || ref.Identifier != "" || ref.VersionID != "" || ref.RevisionID != "" || ref.IconPath != "" || len(ref.Names) != 0 {
		t.Errorf("unpublished resource leaked through article references: %+v", ref)
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	snapshot := communityPostSnapshot{Resources: []communityPostReference{{PublicID: resource.PublicID, Kind: "minecraft.advancement"}}}
	if err := (&Server{db: f.db}).resolveCommunityPostSnapshot(f.ctx, tx, security.Claims{Subject: f.userIDs[f.editor]}, 0, &snapshot); err == nil {
		t.Error("unpublished global resource accepted as a resolved article reference")
	}
}

func TestOCT02PublicRecipeDecoratorsDiscardUnresolvedPresentationFieldsIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	alternative := map[string]any{"id": "missing:unresolved", "detailUrl": "javascript:untrusted",
		"publicId": "fakepublic", "names": map[string]any{"en-US": "Untrusted projected name"},
		"iconPath": "private.png", "iconUrl": "https://untrusted.invalid/pixel"}
	recipe := map[string]any{"layout": map[string]any{"slots": []any{map[string]any{"alternatives": []any{alternative}}}}}
	server := &Server{db: f.db}
	if err := server.decorateRecipeResources(f.ctx, []map[string]any{recipe}, "en-US"); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"detailUrl", "publicId", "names", "iconPath", "iconUrl"} {
		if _, exists := alternative[field]; exists {
			t.Errorf("unresolved recipe retained untrusted %s", field)
		}
	}
	decorated, err := server.decorateCatalysts(f.ctx, []byte(`[{"id":"missing:unresolved","detailUrl":"javascript:untrusted","names":{"en-US":"untrusted"},"iconPath":"private.png"}]`), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"detailUrl", "names", "iconPath"} {
		if _, exists := decorated[0][field]; exists {
			t.Errorf("unresolved catalyst retained untrusted %s", field)
		}
	}
}
func TestOCT02BlueprintAssociationsHideWithdrawnModSourcesIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", "Synthetic blueprint source")
	revisionID := oct02CInsertPublishedExportRevision(t, f, version.PublicID)
	if _, err := f.db.Exec(f.ctx, `insert into mod_identifiers(mod_id,identifier,is_primary) values($1,'test013',true) on conflict do nothing`, f.modID); err != nil {
		t.Fatal(err)
	}
	var blueprintID int64
	var publicID string
	if err := f.db.QueryRow(f.ctx, `insert into blueprints(owner_id,title,source_format,status,review_status) values($1,'Synthetic public blueprint','nbt','ready','approved') returning id,public_id`, f.userIDs[f.editor]).Scan(&blueprintID, &publicID); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		query string
		args  []any
	}{
		{`insert into blueprint_mods(blueprint_id,source_namespace,mod_id) values($1,'test013',$2)`, []any{blueprintID, f.modID}},
		{`insert into blueprint_materials(blueprint_id,block_state,block_id,block_count) values($1,'test013:stone','test013:stone',1)`, []any{blueprintID}},
		{`insert into catalog_import_text_assets(revision_id,asset_path,asset_kind,content_type,sha256,byte_length,text_content) values($1,'synthetic-private-asset.json','data','application/json','synthetic',2,'{}')`, []any{revisionID}},
	} {
		if _, err := f.db.Exec(f.ctx, q.query, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	assertAssociations := func(want bool) {
		t.Helper()
		raw := f.require(t, "", http.MethodGet, "/api/v1/blueprints/"+publicID, nil, http.StatusOK)
		var response struct {
			Data struct {
				RequiredMods   []json.RawMessage `json:"requiredMods"`
				AssetRevisions []json.RawMessage `json:"assetRevisions"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		if (len(response.Data.RequiredMods) > 0) != want || (len(response.Data.AssetRevisions) > 0) != want {
			t.Errorf("public source associations want=%t mods=%d assets=%d body=%s", want, len(response.Data.RequiredMods), len(response.Data.AssetRevisions), raw)
		}
		raw = f.require(t, "", http.MethodGet, "/api/v1/blueprints?q=test013-mod", nil, http.StatusOK)
		var page struct {
			Data struct {
				Items []json.RawMessage `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		if (len(page.Data.Items) > 0) != want {
			t.Errorf("private Mod search match want=%t body=%s", want, raw)
		}
	}
	assertAssociations(true)
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='pending' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	assertAssociations(false)
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='approved' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	assertAssociations(true)
}
