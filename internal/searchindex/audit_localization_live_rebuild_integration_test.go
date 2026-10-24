package searchindex

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

// This combines actual migrated PostgreSQL rows, the business rebuild Worker
// and a real loopback Typesense server. It deliberately seeds an abnormal
// nonapproved canonical localization; normal-user reachability is tested
// separately through HTTP and is not inferred from this fixture.
func TestAuditGRR02RealTypesenseRebuildRemovesLegacyNonapprovedTermsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_TYPESENSE_INTEGRATION") != "1" || os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires explicitly owned loopback PostgreSQL and Typesense")
	}
	address, err := url.Parse(os.Getenv("MCMODS_TEST_TYPESENSE_URL"))
	if err != nil || address.Hostname() != "127.0.0.1" || address.Scheme != "http" || address.User != nil {
		t.Fatal("requires explicit owned loopback Typesense HTTP endpoint")
	}
	key := os.Getenv("MCMODS_TEST_TYPESENSE_API_KEY")
	if key == "" {
		t.Fatal("requires a synthetic Typesense test key")
	}
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fixture, err := database.NewIsolatedTestSchema(ctx, cfg.DB.ConnString(), 4)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := fixture.Close(cleanup); err != nil {
			t.Error(err)
		}
	}()
	db := fixture.Pool
	prefix := "audit_grr02_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	client := New(config.TypesenseConfig{Enabled: true, URL: address.String(), APIKey: key, CollectionPrefix: prefix, Timeout: 3 * time.Second})
	if err := client.Health(ctx); err != nil {
		t.Fatal(err)
	}
	// Capture only names generated under this test's fresh random prefix. Failed
	// rebuild progress is also inspected so a failed test cannot leak collections.
	owned := map[string]bool{}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		rows, err := db.Query(cleanup, `select collection_name from search_index_state union select collection_name from search_index_rebuild_progress`)
		if err != nil {
			t.Error(err)
		} else {
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					t.Error(err)
					break
				}
				owned[name] = true
			}
			if err := rows.Err(); err != nil {
				t.Error(err)
			}
			rows.Close()
		}
		for _, registration := range searchRegistry {
			alias := client.Alias(registration.kind)
			if !strings.HasPrefix(alias, prefix+"_") {
				t.Error("refusing cleanup of alias outside owned prefix")
				continue
			}
			if err := client.request(cleanup, http.MethodDelete, "/aliases/"+url.PathEscape(alias), nil, nil, nil); err != nil && !isStatus(err, 404) {
				t.Error(err)
			}
		}
		for name := range owned {
			if !strings.HasPrefix(name, prefix+"_") {
				t.Error("refusing cleanup of collection outside owned prefix")
				continue
			}
			if err := client.DeleteCollection(cleanup, name); err != nil && !isStatus(err, 404) {
				t.Error(err)
			}
		}
	}()
	var entityID, revisionID int64
	var publicID string
	if err := db.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type,status) values('audit:grr02:live-resource','resource','active') returning id,public_id`).Scan(&entityID, &publicID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path) values($1,'minecraft.item','audit:live_resource','audit','live_resource')`, entityID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `insert into content_revisions(entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash) values('resource',$1,'catalog_editor_resource',$2,1,'{}','synthetic-fixture-hash') returning id`, entityID, publicID).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `update catalog_entities set published_revision_id=$2 where id=$1`, entityID, revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into content_localizations(subject_type,subject_id,catalog_entity_id,locale,name,review_status) values('resource',$1,$1,'en-US','approvedcobalt','approved'),('resource',$1,$1,'zh-CN','legacyprivatequartz','pending'),('resource',$1,$1,'fr-FR','legacyhiddenopal','rejected')`, entityID); err != nil {
		t.Fatal(err)
	}
	oldSchema := collectionSchemas()["resources"]
	oldSchema.Name = client.VersionedCollection("resources", 5)
	owned[oldSchema.Name] = true
	if err := client.CreateCollection(ctx, oldSchema); err != nil {
		t.Fatal(err)
	}
	if err := client.ImportDocuments(ctx, oldSchema.Name, []map[string]any{{"id": documentKey("resource", entityID), "internal_id": entityID, "public_id": publicID, "kind_code": "minecraft.item", "namespace": "audit", "canonical_id": "audit:live_resource", "names": []string{"approvedcobalt", "legacyprivatequartz", "legacyhiddenopal"}, "status": "active", "updated_at": time.Now().Unix()}}); err != nil {
		t.Fatal(err)
	}
	if err := client.UpsertAlias(ctx, client.Alias("resources"), oldSchema.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into search_index_state(collection_kind,schema_version,collection_name) values('resources',5,$1)`, oldSchema.Name); err != nil {
		t.Fatal(err)
	}
	client.SetReady(true)
	search := func(term string, want int) {
		t.Helper()
		result, err := client.Search(ctx, SearchRequest{Collection: "resources", Query: term, QueryBy: []string{"names"}, Page: 1, PerPage: 10})
		if err != nil || result.Found != want {
			t.Fatalf("real search term=%q found=%d want=%d err=%v", term, result.Found, want, err)
		}
		if want == 1 && (len(result.IDs) != 1 || result.IDs[0] != entityID) {
			t.Fatalf("actual resource identity=%v want%d", result.IDs, entityID)
		}
	}
	search("legacyprivatequartz", 1)
	search("legacyhiddenopal", 1)
	worker := NewWorker(db, client)
	if err := worker.rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	var version int
	var current string
	if err := db.QueryRow(ctx, `select schema_version,collection_name from search_index_state where collection_kind='resources'`).Scan(&version, &current); err != nil || version != 6 || current == oldSchema.Name {
		t.Fatalf("rebuild state version=%d new=%v err=%v", version, current != oldSchema.Name, err)
	}
	target, err := client.AliasTarget(ctx, client.Alias("resources"))
	if err != nil || target != current {
		t.Fatalf("resource alias did not switch to committed v6 collection: %v", err)
	}
	search("approvedcobalt", 1)
	search("legacyprivatequartz", 0)
	search("legacyhiddenopal", 0)
	if err := client.request(ctx, http.MethodGet, "/collections/"+url.PathEscape(oldSchema.Name), nil, nil, nil); !isStatus(err, 404) {
		t.Fatalf("owned superseded v5 collection remains: %v", err)
	}
	var rows int
	if err := db.QueryRow(ctx, `select count(*) from content_localizations where catalog_entity_id=$1 and review_status<>'approved'`, entityID).Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("rebuild rewrote abnormal business rows=%d err=%v", rows, err)
	}
	currentBefore := current
	if err := worker.rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `select collection_name from search_index_state where collection_kind='resources'`).Scan(&current); err != nil || current != currentBefore {
		t.Fatalf("repeat rebuild unnecessarily replaced current projection: %v", err)
	}
	t.Log(fmt.Sprintf("real PostgreSQL + Typesense: v5 legacy terms observed, business Worker rebuilt v6, alias switched, approved name retained, two nonapproved names absent, superseded collection removed, rows preserved, repeat idempotent; prefix=%s", prefix))
}
