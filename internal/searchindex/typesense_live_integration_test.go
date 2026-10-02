package searchindex

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestRealTypesenseImportsAndFindsLocalizedTextIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_TYPESENSE_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_TYPESENSE_INTEGRATION=1 with an owned loopback Typesense server")
	}
	address, err := url.Parse(os.Getenv("MCMODS_TEST_TYPESENSE_URL"))
	if err != nil || address.Hostname() != "127.0.0.1" || address.Scheme != "http" || address.User != nil {
		t.Fatal("Typesense live integration requires an explicit owned loopback HTTP endpoint")
	}
	key := os.Getenv("MCMODS_TEST_TYPESENSE_API_KEY")
	if key == "" {
		t.Fatal("Synthetic Typesense test API key is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := New(config.TypesenseConfig{Enabled: true, URL: address.String(), APIKey: key,
		CollectionPrefix: "oct02_" + strconv.FormatInt(time.Now().UnixNano(), 10), Timeout: 3 * time.Second})
	if err = client.Health(ctx); err != nil {
		t.Fatal(err)
	}
	schema := collectionSchemas()["projects"]
	schema.Name = client.VersionedCollection("projects", projectionSchemaVersion)
	if err = client.CreateCollection(ctx, schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := client.request(cleanup, http.MethodDelete, "/aliases/"+url.PathEscape(client.Alias("projects")), nil, nil, nil); err != nil && !isStatus(err, http.StatusNotFound) {
			t.Errorf("remove owned alias: %v", err)
		}
		if err := client.DeleteCollection(cleanup, schema.Name); err != nil {
			t.Errorf("remove owned collection: %v", err)
		}
	})
	document := map[string]any{"id": "mod_1", "internal_id": int64(1), "entity_type": "mod", "public_id": "synthetic",
		"slug": "synthetic", "names": compactStrings([]string{"Primary", "", "Localizedneedle"}),
		"text": compactStrings([]string{"", "Bodyneedle"}), "identifiers": []string{}, "creators": []string{},
		"keywords": []string{}, "categories": []string{}, "minecraft_versions": []string{}, "loaders": []string{},
		"review_status": "approved", "submitted_by": int64(0), "updated_at": time.Now().Unix()}
	if err = client.ImportDocuments(ctx, schema.Name, []map[string]any{document}); err != nil {
		t.Fatal(err)
	}
	if err = client.UpsertAlias(ctx, client.Alias("projects"), schema.Name); err != nil {
		t.Fatal(err)
	}
	client.SetReady(true)
	for _, query := range []string{"Localizedneedle", "Bodyneedle"} {
		result, err := client.Search(ctx, SearchRequest{Collection: "projects", Query: query, QueryBy: []string{"names", "text"},
			FilterBy: "review_status:=approved", Page: 1, PerPage: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.IDs) != 1 || result.IDs[0] != 1 {
			t.Fatalf("query %q returned IDs %v", query, result.IDs)
		}
	}
	if err = client.DeleteDocument(ctx, schema.Name, "mod_1"); err != nil {
		t.Fatal(err)
	}
	result, err := client.Search(ctx, SearchRequest{Collection: "projects", Query: "Localizedneedle", QueryBy: []string{"names"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Found != 0 {
		t.Fatalf("deleted document remains searchable: found=%d", result.Found)
	}
}
