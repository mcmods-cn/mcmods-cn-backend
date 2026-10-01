package searchindex

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

// This test uses the actual Typesense engine and the repository's collection
// schema. It never substitutes an HTTP mock for search/index semantics.
func TestTypesenseRealCollectionImportAliasFilterAndDeleteIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_TYPESENSE_INTEGRATION") != "1" {
		t.Skip("set TYPESENSE_SERVER when starting owned test-services")
	}
	endpoint, key := os.Getenv("MCMODS_TEST_TYPESENSE_URL"), os.Getenv("MCMODS_TEST_TYPESENSE_KEY")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.Host != "127.0.0.1:58108" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		t.Fatal("Typesense integration requires the owned loopback service")
	}
	stored, err := os.ReadFile(filepath.Join(os.Getenv("MCMODS_TEST_STATE"), "typesense-key"))
	if err != nil || key == "" || key != strings.TrimSpace(string(stored)) {
		t.Fatal("Typesense integration credential does not match owned service")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := New(config.TypesenseConfig{Enabled: true, URL: endpoint, APIKey: key, Timeout: 3 * time.Second, CollectionPrefix: fmt.Sprintf("audit_%x", time.Now().UnixNano())})
	if err = client.Health(ctx); err != nil {
		t.Fatal(err)
	}
	var created, aliases []string
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		for _, alias := range aliases {
			if err := client.request(cleanup, "DELETE", "/aliases/"+url.PathEscape(alias), nil, nil, nil); err != nil {
				t.Errorf("remove test-created alias: %v", err)
			}
		}
		for _, name := range created {
			if err := client.DeleteCollection(cleanup, name); err != nil {
				t.Errorf("remove test-created collection: %v", err)
			}
		}
	})
	for kind, schema := range collectionSchemas() {
		schema.Name = client.VersionedCollection(kind)
		if err = client.CreateCollection(ctx, schema); err != nil {
			t.Fatalf("real %s schema: %v", kind, err)
		}
		created = append(created, schema.Name)
		if err = client.UpsertAlias(ctx, client.Alias(kind), schema.Name); err != nil {
			t.Fatal(err)
		}
		aliases = append(aliases, client.Alias(kind))
		actual, err := client.AliasTarget(ctx, client.Alias(kind))
		if err != nil || actual != schema.Name {
			t.Fatalf("alias did not point at imported schema: kind=%s err=%v", kind, err)
		}
	}
	document := func(id int64, status string) map[string]any {
		return map[string]any{"id": fmt.Sprintf("mod_%d", id), "internal_id": id, "entity_type": "mod", "public_id": fmt.Sprintf("p%d", id), "slug": fmt.Sprintf("synthetic-%d", id), "names": []string{"Synthetic 合成测试"}, "text": []string{"Controlled fixture"}, "identifiers": []string{}, "creators": []string{}, "keywords": []string{}, "categories": []string{"building"}, "minecraft_versions": []string{"1.21.1"}, "loaders": []string{"fabric"}, "review_status": status, "submitted_by": int64(5), "updated_at": int64(100 + id)}
	}
	if err = client.ImportDocuments(ctx, client.Alias("projects"), []map[string]any{document(41, "pending"), document(42, "approved")}); err != nil {
		t.Fatal(err)
	}
	query := SearchRequest{Collection: "projects", Query: "Synthetic", QueryBy: []string{"names", "text"}, FilterBy: "entity_type:=mod && review_status:=approved", SortBy: "updated_at:desc", FacetBy: []string{"categories"}, Page: 1, PerPage: 24}
	if _, err = client.Search(ctx, query); !errors.Is(err, ErrUnavailable) {
		t.Fatal("unready projection was exposed")
	}
	client.SetReady(true)
	result, err := client.Search(ctx, query)
	if err != nil || result.Found != 1 || len(result.IDs) != 1 || result.IDs[0] != 42 || result.Facets["categories"]["building"] != 1 {
		t.Fatalf("real filter/facet contract failed: result=%+v err=%v", result, err)
	}
	bad := document(43, "approved")
	bad["updated_at"] = "invalid-numeric-field"
	if err = client.ImportDocuments(ctx, client.Alias("projects"), []map[string]any{bad}); err == nil {
		t.Fatal("real engine rejection was acknowledged as success")
	}
	if err = client.DeleteDocument(ctx, client.Alias("projects"), "mod_42"); err != nil {
		t.Fatal(err)
	}
	result, err = client.Search(ctx, query)
	if err != nil || result.Found != 0 || len(result.IDs) != 0 {
		t.Fatalf("deleted projection still returned: result=%+v err=%v", result, err)
	}
}
