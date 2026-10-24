package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
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
		{ID: " LEGACY:ONE ", Kind: "minecraft.item"}, {ID: "legacy:two", Kind: "minecraft.item"}, {PublicID: resource.PublicID, Kind: "minecraft.item"}, {ID: "legacy:one", Kind: "minecraft.block"}, {ID: "legacy:missing", Kind: "minecraft.item"},
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
	if len(result.Data.Items) != 3 || len(result.Data.Unresolved) != 2 || result.Data.Unresolved[0] != 3 || result.Data.Unresolved[1] != 4 {
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
	if !strings.Contains(string(link), "/data/"+v.PublicID+"/") || !strings.Contains(string(link), "publicId="+resource.PublicID) {
		t.Fatalf("manual shortlink lost resource/version: %s", link)
	}
	if _, err := f.db.Exec(f.ctx, `update mod_content_versions set status='archived' where public_id=$1`, v.PublicID); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", http.MethodGet, "/api/v1/public-links/"+resource.PublicID, nil, 404)
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
