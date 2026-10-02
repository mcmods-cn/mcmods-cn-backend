package searchindex

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPublicSearchDocumentsExcludeUnapprovedSourceMetadataIntegration(t *testing.T) {
	pool, ctx := openSearchRebuildIntegrationPool(t, 15*time.Second)
	// Copy actual columns, defaults, checks, and indexes. No guessed model fields
	// are added to make a projection query compile.
	for _, table := range []string{"mods", "catalog_entities", "game_resources", "catalog_import_revisions", "resource_import_snapshots",
		"catalog_resource_definitions", "mod_resource_version_details", "mod_content_versions", "content_localizations",
		"minecraft_servers", "minecraft_server_mods", "mod_identifiers", "public_routes", "content_popularity_stats"} {
		if _, err := pool.Exec(ctx, "create temporary table "+table+" (like public."+table+" including all)"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `
	 insert into mods(id,project_code,slug,primary_name,review_status) values
	 (101,'r010mod01','r010-public','PublishedParentNeedle','approved'),
	 (102,'r010mod02','r010-private','HiddenParentNeedle','pending');
	 insert into catalog_entities(id,identity_key,public_id,entity_type) values
	 (1001,'r010:shared','r010rs001','resource'),(1002,'r010:private','r010rs002','resource');
	 insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id) values
	 (1001,'minecraft.item','r010:shared','r010','shared',101),
	 (1002,'minecraft.item','r010:private','r010','private',102);
	 insert into catalog_import_revisions(id,mod_id,package_id,job_id,target_version_id,revision_no,status,minecraft_version,loader,
	 exporter_version,source_namespace,is_active) values
	 ('r010-public',101,'synthetic-package','synthetic-job',1,1,'ready','1.21.1','fabric','synthetic','r010',true),
	 ('r010-private',102,'synthetic-package','synthetic-job',2,1,'ready','1.21.1','fabric','synthetic','r010',true);
	 insert into resource_import_snapshots(id,resource_id,revision_id,registry,names) values
	 ('r010-public-snapshot',1001,'r010-public','items','{"en-US":"PublishedResourceNeedle"}'),
	 ('r010-mixed-snapshot',1001,'r010-private','items','{"en-US":"HiddenMixedNeedle"}'),
	 ('r010-private-snapshot',1002,'r010-private','items','{"en-US":"HiddenOnlyNeedle"}');
	 insert into minecraft_servers(id,public_id,slug,address,normalized_address,handshake_host,connect_host,connect_port,name,primary_tag,submitted_by,review_status)
	 values(3001,'r010sv001','r010-server','synthetic.invalid:25565','synthetic.invalid:25565','synthetic.invalid','127.0.0.1',25565,'Synthetic Server','technology',1,'approved');
	 insert into minecraft_server_mods(server_id,raw_mod_id,mod_id) values(3001,'observed_public_id',101),(3001,'observed_private_id',102);
	 insert into mod_identifiers(mod_id,identifier,display_order) values(101,'PublishedIdentifierNeedle',1),(102,'HiddenIdentifierNeedle',1);
	 `); err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(pool, nil)
	resources, err := worker.loadResourceDocuments(ctx, []int64{1001, 1002})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0]["internal_id"] != int64(1001) {
		t.Errorf("unpublished-only resource entered public search: %+v", resources)
	}
	for _, resource := range resources {
		names := fmt.Sprint(resource["names"])
		if strings.Contains(names, "Hidden") {
			t.Errorf("private source name entered public search: %s", names)
		}
	}
	servers, err := worker.loadServerDocuments(ctx, []int64{3001})
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 {
		t.Fatalf("public server documents=%d", len(servers))
	}
	terms := fmt.Sprint(servers[0]["mods"])
	if strings.Contains(terms, "Hidden") || !strings.Contains(terms, "PublishedParentNeedle") ||
		!strings.Contains(terms, "PublishedIdentifierNeedle") || !strings.Contains(terms, "observed_private_id") {
		t.Errorf("public observation must retain raw observed ID and exclude unpublished associated metadata: %s", terms)
	}
	if _, err = pool.Exec(ctx, `update mods set review_status='pending' where id=101`); err != nil {
		t.Fatal(err)
	}
	resources, err = worker.loadResourceDocuments(ctx, []int64{1001, 1002})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 0 {
		t.Errorf("withdrawn parent left resources publicly indexed: %+v", resources)
	}
}

func TestModSearchCompletionDurablyQueuesDependentProjectionsIntegration(t *testing.T) {
	pool, ctx := openSearchRebuildIntegrationPool(t, 15*time.Second)
	for _, table := range []string{"search_index_queue", "minecraft_server_mods", "game_resources", "catalog_import_revisions",
		"resource_import_snapshots", "mod_resource_version_details", "mod_content_versions"} {
		if _, err := pool.Exec(ctx, "create temporary table "+table+" (like public."+table+" including all)"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `
	 insert into minecraft_server_mods(server_id,raw_mod_id,mod_id) values(3001,'synthetic',101);
	 insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id) values
	 (1001,'minecraft.item','r010:one','r010','one',101);
	 insert into search_index_queue(document_type,document_id,operation) values('mod',101,'upsert');`); err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(pool, nil)
	jobs, err := worker.claim(ctx)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("parent claim=%+v err=%v", jobs, err)
	}
	if err = worker.complete(ctx, jobs[0]); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `select count(*) from search_index_queue where
	 (document_type='resource' and document_id=1001) or (document_type='server' and document_id=3001)`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("parent acknowledgment did not durably queue both dependent projections: %d", count)
	}
	if _, err = pool.Exec(ctx, `delete from search_index_queue;
	 insert into search_index_queue(document_type,document_id,operation) values('mod',101,'upsert');
	 alter table search_index_queue add constraint synthetic_reject_children check(document_type='mod');`); err != nil {
		t.Fatal(err)
	}
	jobs, err = worker.claim(ctx)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("failure parent claim=%+v err=%v", jobs, err)
	}
	if err = worker.complete(ctx, jobs[0]); err == nil {
		t.Error("dependent enqueue failure was silently acknowledged")
	}
	if err = pool.QueryRow(ctx, `select count(*) from search_index_queue where document_type='mod' and document_id=101`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("dependent enqueue failure lost the durable parent retry")
	}
}
