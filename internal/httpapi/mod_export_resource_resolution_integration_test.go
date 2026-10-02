package httpapi

import (
	"context"
	"testing"
)

func TestExportResourceResolutionRequiresRequestedKindBeforeRevisionPreferenceIntegration(t *testing.T) {
	db := openGlobalCatalogTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `create temp table catalog_import_revisions(
		id text primary key,mod_id bigint not null,target_version_id bigint,minecraft_version text not null,loader text not null,
		is_active boolean not null,status text not null,activated_at timestamptz,created_at timestamptz not null default now());
		create temp table resource_kinds(code text primary key,family text not null);
		create temp table catalog_entities(id bigint primary key,public_id text not null,status text not null default 'active');
		create temp table game_resources(entity_id bigint primary key,kind_code text not null,canonical_id text not null,namespace text not null);
		create temp table resource_import_snapshots(
			resource_id bigint not null,revision_id text not null,registry text not null default '',icon_path text not null default '',
			preview_path text not null default '',names jsonb not null default '{}'::jsonb);
		create temp table mods(id bigint primary key,slug text not null,status text not null default 'active');
		create temp table mod_content_versions(id bigint primary key,public_id text not null,status text not null default 'active',updated_at timestamptz not null default now());
		create temp table mod_resource_bindings(resource_id bigint not null,mod_id bigint not null);
		create temp table mod_resource_version_details(resource_id bigint not null,version_id bigint not null,status text not null default 'active');
		create temp table mod_resource_version_detail_localizations(resource_id bigint not null,version_id bigint not null,locale text not null,name text not null);
		insert into mods(id,slug) values(1,'preferred-mod'),(2,'correct-kind-mod');
		insert into mod_content_versions(id,public_id) values(11,'preferred-version'),(22,'correct-version');
		insert into catalog_import_revisions(id,mod_id,target_version_id,minecraft_version,loader,is_active,status,activated_at)
		values('preferred',1,11,'1.21.1','neoforge',true,'ready',now()),
			('correct-kind',2,22,'1.21.1','neoforge',true,'ready',now()-interval '1 minute');
		insert into resource_kinds(code,family) values('minecraft.block','block'),('minecraft.item','item');
		insert into catalog_entities(id,public_id) values(101,'block-public'),(202,'item-public');
		insert into game_resources(entity_id,kind_code,canonical_id,namespace)
		values(101,'minecraft.block','shared:part','shared'),(202,'minecraft.item','shared:part','shared');
		insert into resource_import_snapshots(resource_id,revision_id,registry,names)
		values(101,'preferred','blocks','{"en-US":"Wrong block"}'),
			(202,'correct-kind','items','{"en-US":"Correct item"}')`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: db}
	key := exportResourceKey{RevisionID: "preferred", ResourceID: "shared:part", Kind: "item"}
	resolved, err := server.resolveExportResources(ctx, []exportResourceKey{key})
	if err != nil {
		t.Fatal(err)
	}
	source, exists := resolved[key]
	if !exists {
		t.Fatal("requested item was not resolved from the compatible active revision")
	}
	if source.KindCode != "minecraft.item" || source.RevisionID != "correct-kind" || source.PublicID != "item-public" {
		t.Fatalf("revision preference selected the wrong resource kind: %+v", source)
	}
	if _, err = db.Exec(ctx, `update resource_import_snapshots set names='[]'::jsonb where resource_id=202`); err != nil {
		t.Fatal(err)
	}
	if _, err = server.resolveExportResources(ctx, []exportResourceKey{key}); err == nil {
		t.Fatal("resolved resource accepted names with a non-object JSON shape")
	}
	if _, err = db.Exec(ctx, `update resource_import_snapshots set names='{"en-US":"Correct item"}'::jsonb where resource_id=202`); err != nil {
		t.Fatal(err)
	}

	if _, err = db.Exec(ctx, `insert into catalog_entities(id,public_id) values(303,'manual-block-public'),(404,'manual-item-public');
		insert into game_resources(entity_id,kind_code,canonical_id,namespace)
		values(303,'minecraft.block','manual:shared','manual'),(404,'minecraft.item','manual:shared','manual');
		insert into mod_resource_bindings(resource_id,mod_id) values(303,1),(404,2);
		insert into mod_resource_version_details(resource_id,version_id) values(303,11),(404,22);
		insert into mod_resource_version_detail_localizations(resource_id,version_id,locale,name)
		values(303,11,'en-US','Wrong manual block'),(404,22,'en-US','Correct manual item')`); err != nil {
		t.Fatal(err)
	}
	manualKey := exportResourceKey{RevisionID: "preferred", ResourceID: "manual:shared", Kind: "item"}
	resolved, err = server.resolveExportResources(ctx, []exportResourceKey{manualKey})
	if err != nil {
		t.Fatal(err)
	}
	manualSource, exists := resolved[manualKey]
	if !exists || manualSource.KindCode != "minecraft.item" || manualSource.PublicID != "manual-item-public" {
		t.Fatalf("manual fallback selected a different resource kind: %+v exists=%v", manualSource, exists)
	}
}
