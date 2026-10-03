package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func TestOCT02CatalogExactFiltersResolveLegacyCanonicalReferencesIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	if _, err := f.db.Exec(f.ctx, `with entities as (insert into catalog_entities(identity_key,entity_type,status) select 'oct02-exact-filler-'||n,'recipe_type','active' from generate_series(1,70) n returning id,identity_key) insert into recipe_types(entity_id,canonical_id) select id,'aaa:'||identity_key from entities`); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{}
	for _, kind := range []string{"recipe_type", "tag-item", "tag-block"} {
		entityType := "tag"
		if kind == "recipe_type" {
			entityType = kind
		}
		var id int64
		var publicID string
		if err := f.db.QueryRow(f.ctx, `insert into catalog_entities(identity_key,entity_type,status) values($1,$2,'active') returning id,public_id`, "oct02-exact:"+kind, entityType).Scan(&id, &publicID); err != nil {
			t.Fatal(err)
		}
		if kind == "recipe_type" {
			if _, err := f.db.Exec(f.ctx, `insert into recipe_types(entity_id,canonical_id) values($1,'zzzz:exact-type')`, id); err != nil {
				t.Fatal(err)
			}
		} else {
			registry := "minecraft:item"
			if kind == "tag-block" {
				registry = "minecraft:block"
			}
			if _, err := f.db.Exec(f.ctx, `insert into catalog_tags(entity_id,registry,canonical_id) values($1,$2,'zzzz:exact-tag')`, id, registry); err != nil {
				t.Fatal(err)
			}
		}
		expected[kind] = publicID
	}
	for _, item := range []struct{ path, kind string }{
		{"/api/v1/recipe-types?canonicalId=" + url.QueryEscape("zzzz:exact-type"), "recipe_type"},
		{"/api/v1/tags?canonicalId=" + url.QueryEscape("zzzz:exact-tag") + "&registry=" + url.QueryEscape("minecraft:item"), "tag-item"},
		{"/api/v1/tags?canonicalId=" + url.QueryEscape("zzzz:exact-tag") + "&registry=" + url.QueryEscape("minecraft:block"), "tag-block"},
	} {
		for request := 0; request < 2; request++ {
			raw := f.require(t, "", http.MethodGet, item.path, nil, http.StatusOK)
			var response struct {
				Data struct {
					Total int `json:"total"`
					Items []struct {
						PublicID string `json:"publicId"`
					} `json:"items"`
				} `json:"data"`
			}
			if err := json.Unmarshal(raw, &response); err != nil {
				t.Fatal(err)
			}
			if response.Data.Total != 1 || len(response.Data.Items) != 1 || response.Data.Items[0].PublicID != expected[item.kind] {
				t.Fatalf("exact resolver %s returned %s", item.kind, raw)
			}
		}
	}
	for _, path := range []string{"/api/v1/recipe-types?canonicalId=missing:type", "/api/v1/tags?canonicalId=zzzz:exact-tag&registry=missing:registry"} {
		raw := f.require(t, "", http.MethodGet, path, nil, http.StatusOK)
		var response struct {
			Data struct {
				Total int               `json:"total"`
				Items []json.RawMessage `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &response); err != nil || response.Data.Total != 0 || len(response.Data.Items) != 0 {
			t.Fatal(fmt.Sprintf("unknown exact filter body=%s err=%v", raw, err))
		}
	}
}
