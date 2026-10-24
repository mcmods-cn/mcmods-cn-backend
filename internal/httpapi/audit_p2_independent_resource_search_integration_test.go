package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/searchindex"
)

func TestAuditP2RPH003RealTypesenseImportedAndIndependentSourceVisibilityIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_TYPESENSE_INTEGRATION") != "1" {
		t.Skip("explicit task-owned actual Typesense required")
	}
	address, err := url.Parse(os.Getenv("MCMODS_TEST_TYPESENSE_URL"))
	if err != nil || address.Scheme != "http" || address.Hostname() != "127.0.0.1" || address.User != nil {
		t.Fatal("explicit task-owned loopback Typesense required")
	}
	key := os.Getenv("MCMODS_TEST_TYPESENSE_API_KEY")
	if key == "" {
		t.Fatal("owned synthetic Typesense key required")
	}
	f := newAuditFixGRFixture(t)
	independent := auditP2PublishedGlobalResource(t, f.test013Fixture, "p2-real-independent")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "global_resource.list")
	_, revision := f.imported(t, auditFixGRZip(t, map[string]string{"registries/items.json": `{"registry":"items","entries":[{"id":"example:p2_real_import","namespace":"example","names":{"en-US":"P2 imported resource"},"max_stack_size":64}]}`}))
	var independentID, importedID int64
	var importedPublic string
	if err = f.db.QueryRow(f.ctx, `select id from catalog_entities where public_id=$1`, independent.ObjectPublicID).Scan(&independentID); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(f.ctx, `select resource.entity_id,entity.public_id from game_resources resource join catalog_entities entity on entity.id=resource.entity_id where resource.canonical_id='example:p2_real_import' and resource.kind_code='minecraft.item'`).Scan(&importedID, &importedPublic); err != nil {
		t.Fatal(err)
	}
	prefix := "p2_exporter_resource_" + fmt.Sprint(time.Now().UnixNano())
	client := searchindex.New(config.TypesenseConfig{Enabled: true, URL: address.String(), APIKey: key, CollectionPrefix: prefix, Timeout: 3 * time.Second})
	if err = client.Health(f.ctx); err != nil {
		t.Fatal("owned Typesense health failed")
	}
	collection, alias := client.VersionedCollection("resources", 6), client.Alias("resources")
	fields := []searchindex.Field{{Name: "internal_id", Type: "int64"}, {Name: "updated_at", Type: "int64"}, {Name: "kind_code", Type: "string", Facet: true}, {Name: "namespace", Type: "string", Facet: true}, {Name: "canonical_id", Type: "string", Infix: true}, {Name: "public_id", Type: "string", Infix: true}, {Name: "names", Type: "string[]", Infix: true}}
	if err = client.CreateCollection(f.ctx, searchindex.CollectionSchema{Name: collection, Fields: fields, DefaultSortingField: "updated_at"}); err != nil {
		t.Fatal("create owned collection failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r, e := http.NewRequestWithContext(ctx, http.MethodDelete, address.String()+"/aliases/"+url.PathEscape(alias), nil)
		if e != nil {
			t.Error(e)
			return
		}
		r.Header.Set("X-TYPESENSE-API-KEY", key)
		response, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Error("delete owned alias failed")
			return
		}
		response.Body.Close()
		if response.StatusCode != 200 && response.StatusCode != 404 {
			t.Errorf("owned alias cleanup status=%d", response.StatusCode)
		}
		if e = client.DeleteCollection(ctx, collection); e != nil {
			t.Error("delete owned collection failed")
		}
	})
	// Both candidates remain in the actual index through all state changes.
	if err = client.ImportDocuments(f.ctx, collection, []map[string]any{
		{"id": fmt.Sprint(independentID), "internal_id": independentID, "updated_at": int64(1), "kind_code": "minecraft.item", "namespace": "audit", "canonical_id": "audit:p2-real-independent", "public_id": independent.ObjectPublicID, "names": []string{"P2 directory"}},
		{"id": fmt.Sprint(importedID), "internal_id": importedID, "updated_at": int64(2), "kind_code": "minecraft.item", "namespace": "example", "canonical_id": "example:p2_real_import", "public_id": importedPublic, "names": []string{"P2 directory"}},
	}); err != nil {
		t.Fatal("import synthetic owned candidate docs failed")
	}
	if err = client.UpsertAlias(f.ctx, alias, collection); err != nil {
		t.Fatal("set owned alias failed")
	}
	client.SetReady(true)
	f.server.search = client
	check := func(label string, want int) {
		t.Helper()
		if indexed := f.server.searchResourcePage(f.ctx, "p2", "", "", 1, 0); !indexed.Used || indexed.Total != want {
			t.Fatalf("%s real indexed page not used/count=%+v want=%d", label, indexed, want)
		}
		seen := map[string]bool{}
		for offset := 0; offset <= want; offset++ {
			page := decodeTEST022Data[struct {
				Total int              `json:"total"`
				Items []map[string]any `json:"items"`
			}](t, f.require(t, f.denied, http.MethodGet, fmt.Sprintf("/api/v1/catalog/resources?q=p2&limit=1&offset=%d", offset), nil, 200))
			expected := 1
			if offset == want {
				expected = 0
			}
			if page.Total != want || len(page.Items) != expected {
				t.Fatalf("%s offset=%d total=%d/items=%d", label, offset, page.Total, len(page.Items))
			}
			for _, item := range page.Items {
				id, _ := item["publicId"].(string)
				if id == "" || seen[id] {
					t.Fatal("missing/duplicate public resource across pages")
				}
				seen[id] = true
			}
		}
		if !seen[independent.ObjectPublicID] {
			t.Fatal("unrelated approved global object was hidden")
		}
		stale, e := client.Search(f.ctx, searchindex.SearchRequest{Collection: "resources", Query: "p2", QueryBy: []string{"names"}})
		if e != nil || stale.Found != 2 {
			t.Fatalf("actual stale index prerequisite found=%d err=%v", stale.Found, e)
		}
	}
	check("pending import", 1)
	f.reviewGR(t, revision, "approved")
	check("approved import", 2)
	for _, state := range []string{"pending", "rejected", "approved"} {
		if _, err = f.db.Exec(f.ctx, `update mods set review_status=$2 where id=$1`, f.modID, state); err != nil {
			t.Fatal(err)
		}
		want := 1
		if state == "approved" {
			want = 2
		}
		check("source "+state, want)
	}
	t.Log("REAL_TYPESENSE resources stale2 throughout; current pending import/hidden source omitted from items and total, page union stable, independent approved global source preserved")
}
