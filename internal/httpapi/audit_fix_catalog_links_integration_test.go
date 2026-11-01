package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAuditFixRPH005AliasInputMappingAndManualResourceShortlink(t *testing.T) {
	f := newTEST013Fixture(t)
	v, _, resource := auditRPHResource(t, f)
	if _, err := f.db.Exec(f.ctx, `insert into game_resource_aliases(kind_code,alias_id,resource_id)
 select 'minecraft.item',alias,entity.id from catalog_entities entity cross join unnest(array['legacy:one','legacy:two']) alias where entity.public_id=$1`, resource.PublicID); err != nil {
		t.Fatal(err)
	}
	raw := f.require(t, "", http.MethodPost, "/api/v1/catalog/resource-presentations", catalogPresentationBatchRequest{Items: []catalogPresentationBatchReference{
		{ID: " LEGACY:ONE ", Kind: "minecraft.item"}, {ID: "legacy:two", Kind: "minecraft.item"}, {PublicID: resource.PublicID, Kind: "minecraft.item"}, {ID: "legacy:one", Kind: "minecraft.block"}, {ID: "legacy:missing", Kind: "minecraft.item"}, {PublicID: resource.PublicID, Kind: "minecraft.block"},
	}}, http.StatusOK)
	var result struct {
		Data struct {
			Items      []catalogPresentationBatchItem `json:"items"`
			Unresolved []int                          `json:"unresolvedInputIndices"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data.Items) != 3 || len(result.Data.Unresolved) != 3 || result.Data.Unresolved[0] != 3 || result.Data.Unresolved[1] != 4 || result.Data.Unresolved[2] != 5 {
		t.Fatalf("mapping: %s", raw)
	}
	seen := map[int]bool{}
	for _, item := range result.Data.Items {
		if item.PublicID != resource.PublicID || item.InputIndex < 0 || item.InputIndex > 2 || seen[item.InputIndex] {
			t.Fatalf("invalid mapped item: %+v", item)
		}
		seen[item.InputIndex] = true
	}
	link := f.require(t, "", http.MethodGet, "/api/v1/public-links/"+resource.PublicID, nil, 200)
	var resolved struct {
		Data struct {
			Target string `json:"target"`
		} `json:"data"`
	}
	if err := json.Unmarshal(link, &resolved); err != nil {
		t.Fatal(err)
	}
	if want := "/mods/test013-mod/resources/" + resource.PublicID + "?version=" + v.PublicID; resolved.Data.Target != want {
		t.Fatalf("manual shortlink must use the existing exact resource detail route: got=%s want=%s", resolved.Data.Target, want)
	}
	if _, err := f.db.Exec(f.ctx, `update mod_content_versions set status='archived' where public_id=$1`, v.PublicID); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", http.MethodGet, "/api/v1/public-links/"+resource.PublicID, nil, 404)
}

func TestAuditFixSL003ImportedLinkUsesCurrentContentVersionAndSourceState(t *testing.T) {
	f := newAuditFixGRFixture(t)
	_, revision := f.imported(t, auditFixGRZip(t, nil))
	f.reviewGR(t, revision, "approved")
	var publicID string
	if err := f.db.QueryRow(f.ctx, `select entity.public_id from catalog_entities entity join game_resources resource on resource.entity_id=entity.id where resource.canonical_id='example:audit_item' and resource.kind_code='minecraft.item'`).Scan(&publicID); err != nil {
		t.Fatal(err)
	}
	link := f.require(t, "", http.MethodGet, "/api/v1/public-links/"+publicID, nil, 200)
	var resolved struct {
		Data struct {
			Target string `json:"target"`
		} `json:"data"`
	}
	if err := json.Unmarshal(link, &resolved); err != nil {
		t.Fatal(err)
	}
	if want := "/mods/audit-gr-" + f.modCode + "/resources/" + publicID + "?version=" + f.versionPublic; resolved.Data.Target != want {
		t.Fatalf("import link must use resource and target content version, never import revision: got=%s want=%s", resolved.Data.Target, want)
	}
	f.require(t, "", http.MethodGet, "/api/v1/mods/audit-gr-"+f.modCode+"/content-resources/"+publicID, nil, 200)
	// Isolated source-state fixtures test current visibility, without claiming
	// that these SQL transitions are a public withdrawal API.
	if _, err := f.db.Exec(f.ctx, `update catalog_import_revisions set is_active=false where id=$1`, revision); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", http.MethodGet, "/api/v1/public-links/"+publicID, nil, 404)
	if _, err := f.db.Exec(f.ctx, `update catalog_import_revisions set is_active=true where id=$1`, revision); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", http.MethodGet, "/api/v1/public-links/"+publicID, nil, 200)
	if _, err := f.db.Exec(f.ctx, `update mod_content_versions set status='archived' where id=$1`, f.versionID); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", http.MethodGet, "/api/v1/public-links/"+publicID, nil, 404)
}

func TestAuditFixSL002EconomyLinksUseExistingWorkspace(t *testing.T) {
	f := newTEST013Fixture(t)
	if _, err := f.db.Exec(f.ctx, `insert into task_definitions(code,name) values('audit-sl002','Audit task')`); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"currency", "shop_item", "task"} {
		var publicID string
		err := f.db.QueryRow(f.ctx, `select public_id from public_routes where entity_type=$1 order by id limit 1`, kind).Scan(&publicID)
		if err != nil {
			t.Fatal(err)
		}
		raw := f.require(t, "", http.MethodGet, "/api/v1/public-links/"+publicID, nil, 200)
		var response struct {
			Data struct {
				Target string `json:"target"`
			} `json:"data"`
		}
		if err = json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		if response.Data.Target != "/user?section=economy&objectType="+kind+"&publicId="+publicID {
			t.Fatalf("%s route=%s", kind, raw)
		}
	}
}
