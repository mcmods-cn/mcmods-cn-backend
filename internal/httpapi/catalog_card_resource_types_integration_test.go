package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestPublicRecipeRenderKeepsManualCandidateResourceKindIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	createEntity := func(identity catalogIdentity, kind string) int64 {
		t.Helper()
		var id int64
		if err := tx.QueryRow(f.ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status)
			values($1,$2,$3,'active') returning id`, identity.ID, identity.PublicID, kind).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	recipeType := createEntity(recipeTypeIdentity("example:mixing"), "recipe_type")
	recipe := recipeIdentity("", "example:mixing", "example:manual_mixing", true)
	recipeID := createEntity(recipe, "recipe")
	templateID := createEntity(catalogIdentity{ID: "test:catalog-card:template", PublicID: randomCatalogPublicID()}, "recipe_template")
	itemID := createEntity(resourceIdentity("minecraft.item", "example:shared_name"), "resource")
	fluid := resourceIdentity("minecraft.fluid", "example:shared_name")
	fluidID := createEntity(fluid, "resource")
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path) values
			($1,'minecraft.item','example:shared_name','example','shared_name'),
			($2,'minecraft.fluid','example:shared_name','example','shared_name')`, []any{itemID, fluidID}},
		{`insert into mod_resource_bindings(resource_id,mod_id) values($1,$3),($2,$3)`, []any{itemID, fluidID, f.modID}},
		{`insert into mod_resource_version_details(resource_id,version_id,default_locale,status)
			values($1,$3,'en-US','active'),($2,$3,'en-US','active')`, []any{itemID, fluidID, f.versionID}},
		{`insert into recipe_types(entity_id,canonical_id) values($1,'example:mixing')`, []any{recipeType}},
		{`insert into recipes(entity_id,recipe_type_id,canonical_source_id,semantic_fingerprint,identity_source)
			values($1,$2,'example:manual_mixing','manual-mixing-test','manual')`, []any{recipeID, recipeType}},
		{`insert into recipe_layout_templates(entity_id,recipe_type_id,template_key,canvas_width,canvas_height,image_scale)
			values($1,$2,'mixing-test',50,50,1)`, []any{templateID, recipeType}},
		{`insert into recipe_template_slots(identity_key,template_id,slot_key,role,ordinal,x,y,width,height)
			values('test:catalog-card:output',$1,'output','output',0,1,1,16,16)`, []any{templateID}},
		{`insert into recipe_definitions(recipe_id,template_id,definition) values($1,$2,'{}')`, []any{recipeID, templateID}},
		{`insert into recipe_bindings(identity_key,recipe_id,template_slot_id,ordinal)
			select 'test:catalog-card:binding',$1,id,0 from recipe_template_slots where template_id=$2`, []any{recipeID, templateID}},
		{`insert into recipe_binding_candidates(identity_key,binding_id,candidate_index,resource_id,amount,probability)
			select 'test:catalog-card:candidate',id,0,$1,250,0 from recipe_bindings where recipe_id=$2`, []any{fluidID, recipeID}},
	} {
		if _, err := tx.Exec(f.ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}

	raw := f.require(t, "", http.MethodGet, "/api/v1/catalog/recipes/"+recipe.PublicID+"/render", nil, http.StatusOK)
	result := decodeTEST022Data[struct {
		Layout struct {
			Slots []struct {
				Alternatives []map[string]any
			}
		}
	}](t, raw)
	if len(result.Layout.Slots) != 1 || len(result.Layout.Slots[0].Alternatives) != 1 {
		t.Fatalf("expected one canonical result slot and candidate: %#v", result)
	}
	candidate := result.Layout.Slots[0].Alternatives[0]
	if candidate["kind"] != "minecraft.fluid" || candidate["publicId"] != fluid.PublicID ||
		candidate["registry"] != "fluids" || candidate["namespace"] != "example" ||
		candidate["amount"] != float64(250) || candidate["probability"] != float64(0) {
		t.Fatalf("canonical fluid resolved incorrectly: %#v", candidate)
	}
}

func TestLootCardsRetainCanonicalAndImportedSummaryKeysIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	icon, err := base64.StdEncoding.DecodeString(embeddedIconTestPNG(t, 32, 32))
	if err != nil {
		t.Fatal(err)
	}
	const iconPath = "icons/items/32/example/audit_item.png"
	_, revision := f.imported(t, auditFixGRZip(t, map[string]string{
		iconPath:                    string(icon),
		"worldgen/loot_tables.json": `{"schema_version":"mcmods-loot-tables/v2","loot_tables":[{"id":"example:chests/audit","namespace":"example","type":"minecraft:chest","pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"example:audit_item"}]}]}]}`,
	}))
	f.reviewGR(t, revision, "approved")
	var sectionPublicID string
	var resourceID int64
	if err = f.db.QueryRow(f.ctx, `select section.public_id,placement.resource_id
		from mod_content_sections section join mod_content_templates template on template.id=section.template_id
		join mod_content_section_resources placement on placement.section_id=section.id
		where section.version_id=$1 and template.code='loot_table' and section.status='active'`, f.versionID).
		Scan(&sectionPublicID, &resourceID); err != nil {
		t.Fatal(err)
	}
	for _, source := range []struct {
		name string
		data string
	}{
		{"canonical", `{"possibleItemIds":["example:audit_item"]}`},
		{"older_import_shape", `{"possible_item_ids":["example:audit_item"]}`},
	} {
		t.Run(source.name, func(t *testing.T) {
			if _, err := f.db.Exec(f.ctx, `update mod_resource_version_details set definition=$3::jsonb
				where resource_id=$1 and version_id=$2`, resourceID, f.versionID, source.data); err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/mods/audit-gr-" + f.modCode + "/content-sections/" + sectionPublicID + "/resources"
			result := decodeTEST022Data[struct {
				Items []struct {
					Definition struct {
						PossibleItemIDs  []string `json:"possible_item_ids"`
						PreviewResources []map[string]string
					}
				}
			}](t, f.require(t, "", http.MethodGet, path, nil, http.StatusOK))
			if len(result.Items) != 1 {
				t.Fatalf("expected one public loot card: %#v", result)
			}
			definition := result.Items[0].Definition
			wantPreview := []map[string]string{{"sourceRevisionId": revision, "iconPath": iconPath}}
			if !reflect.DeepEqual(definition.PossibleItemIDs, []string{"example:audit_item"}) ||
				!reflect.DeepEqual(definition.PreviewResources, wantPreview) {
				t.Fatalf("loot summary or icon was lost: %#v", definition)
			}
		})
	}
}

func TestResourcePresentationUsesPublishedDisplayNameIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	identity := resourceIdentity("minecraft.item", "example:display_name")
	var entityID int64
	if err := f.db.QueryRow(f.ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status,default_locale)
		values($1,$2,'resource','active','zh-CN') returning id`, identity.ID, identity.PublicID).Scan(&entityID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,resolved)
		values($1,'minecraft.item','example:display_name','example','display_name',true)`, entityID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into content_localizations(subject_type,subject_id,catalog_entity_id,locale,name,review_status)
		values('resource',$1,$1,'zh-CN','Draft translation','pending'),('resource',$1,$1,'en-US','Published translation','approved')`, entityID); err != nil {
		t.Fatal(err)
	}
	raw := f.require(t, "", http.MethodGet, "/api/v1/catalog/resource-presentation?ref="+identity.PublicID+"&locale=zh-CN", nil, http.StatusOK)
	display := decodeTEST022Data[map[string]any](t, raw)
	if display["registry"] != "items" || display["namespace"] != "example" {
		t.Errorf("resource family and identifier namespace = %q/%q", display["registry"], display["namespace"])
	}
	if display["name"] != "Published translation" || display["locale"] != "en-US" {
		t.Fatalf("expected the approved fallback language, got name=%q locale=%q", display["name"], display["locale"])
	}
}

func TestCatalogReferenceSelectionRequiresAnUnambiguousVisibleIdentity(t *testing.T) {
	f := newAuditFixGRFixture(t)
	const identifier = "example:shared_presentation"
	var fluids, solidTag string
	for _, kind := range []string{"minecraft.item", "minecraft.fluid"} {
		identity := resourceIdentity(kind, identifier)
		if _, err := f.db.Exec(f.ctx, `with entity as (
			insert into catalog_entities(identity_key,public_id,entity_type,status) values($1,$2,'resource','active') returning id)
			insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path)
			select id,$3,$4,'example','shared_presentation' from entity`, identity.ID, identity.PublicID, kind, identifier); err != nil {
			t.Fatal(err)
		}
		if kind == "minecraft.fluid" {
			fluids = identity.PublicID
		}
	}
	for _, registry := range []string{"minecraft:item", "minecraft:fluid"} {
		identity := tagIdentity(registry, identifier)
		if _, err := f.db.Exec(f.ctx, `with entity as (
			insert into catalog_entities(identity_key,public_id,entity_type,status) values($1,$2,'tag','active') returning id)
			insert into catalog_tags(entity_id,registry,canonical_id) select id,$3,$4 from entity`, identity.ID, identity.PublicID, registry, identifier); err != nil {
			t.Fatal(err)
		}
		if registry == "minecraft:item" {
			solidTag = identity.PublicID
		}
	}

	t.Run("single", func(t *testing.T) {
		path := "/api/v1/catalog/resource-presentation?ref="
		var conflict apiResponse
		if err := json.Unmarshal(f.require(t, "", http.MethodGet, path+identifier, nil, http.StatusConflict), &conflict); err != nil || conflict.Code != "resource_reference_ambiguous" {
			t.Fatalf("ambiguous reference code = %q: %v", conflict.Code, err)
		}
		for _, ref := range []string{identifier + "&kind=minecraft.fluid", fluids} {
			result := decodeTEST022Data[map[string]string](t, f.require(t, "", http.MethodGet, path+ref, nil, http.StatusOK))
			if result["publicId"] != fluids || result["registry"] != "fluids" {
				t.Errorf("resolved %s to %#v", ref, result)
			}
		}
		f.require(t, "", http.MethodGet, path+identifier+"&kind=minecraft.block", nil, http.StatusNotFound)
	})
	t.Run("batch", func(t *testing.T) {
		items := []catalogPresentationBatchReference{
			{ID: identifier}, {ID: identifier, Kind: "minecraft.fluid"}, {ID: identifier, PublicID: fluids},
			{ID: identifier, Registry: "fluids"}, {ID: identifier, Kind: "tag"}, {ID: identifier, Kind: "tag", Registry: "minecraft:item"},
		}
		got := decodeTEST022Data[struct {
			Items                  []catalogPresentationBatchItem
			UnresolvedInputIndices []int
		}](t, f.require(t, "", http.MethodPost, "/api/v1/catalog/resource-presentations", catalogPresentationBatchRequest{Locale: "en-US", Items: items}, http.StatusOK))
		if !reflect.DeepEqual(got.UnresolvedInputIndices, []int{0, 4}) {
			t.Errorf("unresolved input positions = %v", got.UnresolvedInputIndices)
		}
		matched := map[int]string{}
		for _, item := range got.Items {
			matched[item.InputIndex] = item.PublicID
		}
		if !reflect.DeepEqual(matched, map[int]string{1: fluids, 2: fluids, 3: fluids, 5: solidTag}) {
			t.Errorf("resolved input positions and IDs = %v", matched)
		}
	})
}
