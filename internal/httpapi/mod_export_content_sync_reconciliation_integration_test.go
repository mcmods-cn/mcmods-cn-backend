package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestImportedContentSyncReconcilesOnlyItsSourceScopeIntegration(t *testing.T) {
	db := openGlobalCatalogTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `create temp table mod_content_versions(id bigint primary key,mod_id bigint not null,status text not null);
		create temp table catalog_import_revisions(id text primary key,source_namespace text not null,source_kind text not null,revision_no bigint not null);
		create temp table game_resources(entity_id bigint primary key,kind_code text not null,canonical_id text not null);
		create temp table resource_import_snapshots(
			id text primary key,resource_id bigint not null,revision_id text not null,entry_type_code text not null default 'default',
			definition_schema_version smallint not null default 1,names jsonb not null default '{}'::jsonb,data jsonb not null default '{}'::jsonb,
			icon_path text not null default '',preview_path text not null default '');
		create temp table catalog_import_media(revision_id text not null,asset_path text not null,oss_file_id bigint not null,primary key(revision_id,asset_path));
		create temp table mod_resource_bindings(resource_id bigint primary key,mod_id bigint not null);
		create temp table mod_resource_version_details(
			resource_id bigint not null,version_id bigint not null,entry_type_code text not null default 'default',
			definition_schema_version smallint not null default 1,default_locale text not null default 'en-US',definition jsonb not null default '{}'::jsonb,
			icon_file_id bigint,render_file_id bigint,projection_source text not null default 'manual',
			import_source_namespace text not null default '',import_source_kind text not null default '',import_revision_id text not null default '',
			status text not null default 'active',published_revision_id bigint,created_by bigint,updated_by bigint,
			created_at timestamptz not null default now(),updated_at timestamptz not null default now(),primary key(resource_id,version_id));
		create temp table mod_resource_version_detail_localizations(
			resource_id bigint not null,version_id bigint not null,locale text not null,name text not null default '',
			summary text not null default '',content_markdown text not null default '',provenance text not null default 'human',
			primary key(resource_id,version_id,locale));
		create temp table mod_content_templates(
			id bigint primary key,code text not null,builtin boolean not null,default_locale text not null,default_display_mode text not null);
		create temp table mod_content_sections(
			id bigserial primary key,mod_id bigint not null,version_id bigint not null,template_id bigint not null,parent_id bigint,
			system_key text not null default '',default_locale text not null,display_mode text not null,ordinal integer not null,status text not null,
			created_by bigint,updated_by bigint,updated_at timestamptz not null default now(),unique(version_id,parent_id,ordinal));
		create unique index test_mod_content_sections_system_key on mod_content_sections(version_id,parent_id,system_key) where system_key<>'' and status='active';
		create temp table mod_content_section_localizations(
			section_id bigint not null,locale text not null,name text not null,description text not null,primary key(section_id,locale));
		create temp table mod_content_section_resources(
			section_id bigint not null,version_id bigint not null,resource_id bigint not null,placement_identity_key text not null default '',
			ordinal integer not null,placement_source text not null,primary key(section_id,version_id,resource_id),
			unique(section_id,version_id,ordinal),unique(version_id,resource_id));
		create temp table game_resource_asset_bindings(snapshot_id text primary key,item_resource_id bigint,block_resource_id bigint);
		insert into mod_content_versions values(100,500,'active');
		insert into catalog_import_revisions values
			('a1','alpha','mcmods_exporter',1),('a2','alpha','mcmods_exporter',2),('b1','beta','mcmods_exporter',1);
		insert into game_resources values
			(1,'minecraft.item','alpha:keep'),(2,'minecraft.item','alpha:old_name'),
			(3,'minecraft.item','beta:neighbor'),(4,'minecraft.item','alpha:new_name'),(5,'minecraft.item','alpha:manual');
		insert into resource_import_snapshots(id,resource_id,revision_id,names,data,icon_path) values
			('a1-keep',1,'a1','{"en_US":"Old","zh_CN":"旧"}','{"keep":1,"removed":2}','old.png'),
			('a1-old',2,'a1','{"en_US":"Old name"}','{"old":true}',''),
			('b1-neighbor',3,'b1','{"en_US":"Neighbor"}','{"beta":true}',''),
			('a2-keep',1,'a2','{"en_US":"New"}','{"keep":10,"added":3}',''),
			('a2-new',4,'a2','{"en_US":"New name"}','{"new":true}',''),
			('a2-manual',5,'a2','{"en_US":"Imported manual"}','{"manual":false,"incoming":true}','');
		insert into catalog_import_media values('a1','old.png',700);
		insert into mod_content_templates values(10,'item_block',true,'en-US','compact')`); err != nil {
		t.Fatal(err)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = syncImportedResourcesToContentVersionTx(ctx, tx, []string{"a1", "b1"}, 100, true, 41); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `insert into mod_resource_bindings values(5,500);
		insert into mod_resource_version_details(resource_id,version_id,definition,projection_source,status)
		values(5,100,'{"manual":true}','manual','active');
		insert into mod_resource_version_detail_localizations values
			(1,100,'fr-FR','Nom manuel','','','human'),(5,100,'en-US','Manual name','','','human')`); err != nil {
		t.Fatal(err)
	}

	tx, err = db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = syncImportedResourcesToContentVersionTx(ctx, tx, []string{"a2"}, 100, false, 42); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var status, source, namespace, revision string
	var definitionRaw []byte
	var iconFileID *int64
	if err = db.QueryRow(ctx, `select status,projection_source,import_source_namespace,import_revision_id,definition,icon_file_id
		from mod_resource_version_details where resource_id=1 and version_id=100`).
		Scan(&status, &source, &namespace, &revision, &definitionRaw, &iconFileID); err != nil {
		t.Fatal(err)
	}
	var definition map[string]any
	if err = json.Unmarshal(definitionRaw, &definition); err != nil {
		t.Fatal(err)
	}
	if status != "active" || source != "import" || namespace != "alpha" || revision != "a2" || iconFileID != nil || definition["keep"] != float64(10) || definition["added"] != float64(3) {
		t.Fatalf("replacement projection mismatch: status=%s source=%s namespace=%s revision=%s icon=%v definition=%#v", status, source, namespace, revision, iconFileID, definition)
	}
	if _, exists := definition["removed"]; exists {
		t.Fatalf("removed imported field survived replacement: %#v", definition)
	}

	var oldStatus, betaStatus, betaRevision, manualSource string
	var manualDefinition []byte
	if err = db.QueryRow(ctx, `select status from mod_resource_version_details where resource_id=2 and version_id=100`).Scan(&oldStatus); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `select status,import_revision_id from mod_resource_version_details where resource_id=3 and version_id=100`).Scan(&betaStatus, &betaRevision); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `select projection_source,definition from mod_resource_version_details where resource_id=5 and version_id=100`).Scan(&manualSource, &manualDefinition); err != nil {
		t.Fatal(err)
	}
	if oldStatus != "archived" || betaStatus != "active" || betaRevision != "b1" || manualSource != "manual" || string(manualDefinition) != `{"manual": true}` {
		t.Fatalf("scope/manual isolation failed: old=%s beta=%s/%s manual=%s/%s", oldStatus, betaStatus, betaRevision, manualSource, manualDefinition)
	}

	rows, err := db.Query(ctx, `select locale,name,provenance from mod_resource_version_detail_localizations where resource_id=1 and version_id=100 order by locale`)
	if err != nil {
		t.Fatal(err)
	}
	localizations := map[string]string{}
	for rows.Next() {
		var locale, name, provenance string
		if err = rows.Scan(&locale, &name, &provenance); err != nil {
			t.Fatal(err)
		}
		localizations[locale] = name + ":" + provenance
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if len(localizations) != 2 || localizations["en-us"] != "New:import" || localizations["fr-FR"] != "Nom manuel:human" {
		t.Fatalf("import locale replacement did not preserve only human facts: %#v", localizations)
	}

	var oldPlacement, betaPlacement, newPlacement, manualPlacement int
	if err = db.QueryRow(ctx, `select
		count(*) filter(where resource_id=2)::int,count(*) filter(where resource_id=3)::int,
		count(*) filter(where resource_id=4)::int,count(*) filter(where resource_id=5)::int
		from mod_content_section_resources where version_id=100`).
		Scan(&oldPlacement, &betaPlacement, &newPlacement, &manualPlacement); err != nil {
		t.Fatal(err)
	}
	if oldPlacement != 0 || betaPlacement != 1 || newPlacement != 1 || manualPlacement != 0 {
		t.Fatalf("placement reconciliation old=%d beta=%d new=%d manual=%d", oldPlacement, betaPlacement, newPlacement, manualPlacement)
	}
}

func TestImportedResourceClassificationExpressionsCoverAllMappingsIntegration(t *testing.T) {
	db := openGlobalCatalogTestDB(t)
	ctx := context.Background()
	templateExpected := map[string]string{
		"minecraft.item": "item_block", "minecraft.block": "item_block", "minecraft.fluid": "fluid",
		"minecraft.dimension": "dimension", "minecraft.biome": "biome", "minecraft.entity_type": "entity",
		"minecraft.enchantment": "enchantment", "minecraft.mob_effect": "mob_effect", "minecraft.potion": "mob_effect",
		"minecraft.natural_generation": "natural_generation", "minecraft.structure": "world_structure",
		"minecraft.key_mapping": "key_mapping", "minecraft.advancement": "advancement",
		"minecraft.loot_table": "loot_table", "minecraft.game_setting": "game_setting",
		"mekanism.gas": "chemical", "mekanism.infusion": "chemical", "mekanism.pigment": "chemical", "mekanism.slurry": "chemical",
	}
	values := make([]string, 0, len(templateExpected))
	for kind := range templateExpected {
		values = append(values, "('"+kind+"'::text)")
	}
	rows, err := db.Query(ctx, `select resource.kind_code,`+importedContentTemplateCodeSQL("resource")+
		` from (values `+strings.Join(values, ",")+`) resource(kind_code)`)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for rows.Next() {
		var kind, template string
		if err = rows.Scan(&kind, &template); err != nil {
			t.Fatal(err)
		}
		seen[kind] = template
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if len(seen) != len(templateExpected) {
		t.Fatalf("classified %d kinds, want %d", len(seen), len(templateExpected))
	}
	for kind, expected := range templateExpected {
		if seen[kind] != expected {
			t.Errorf("kind %s classified as %q, want %q", kind, seen[kind], expected)
		}
	}

	lootExpected := map[string]string{
		"fishing": "fishing", "blocks": "blocks", "chests": "chests", "entities": "entities",
		"archaeology": "archaeology", "equipment": "equipment", "gameplay": "gameplay", "other": "other",
	}
	lootRows, err := db.Query(ctx, `select snapshot.test_key,`+importedLootCategorySQL("snapshot", "snapshot")+` from (values
		('fishing'::text,'minecraft:gameplay/fishing/fish'::text,'{}'::jsonb),
		('blocks','minecraft:any','{"category":"block"}'::jsonb),
		('chests','minecraft:chests/simple','{}'::jsonb),
		('entities','minecraft:any','{"category":"entity"}'::jsonb),
		('archaeology','minecraft:archaeology/desert','{}'::jsonb),
		('equipment','minecraft:any','{"category":"equipment"}'::jsonb),
		('gameplay','minecraft:gameplay/barter','{}'::jsonb),
		('other','minecraft:misc/example','{}'::jsonb)
	) snapshot(test_key,canonical_id,data)`)
	if err != nil {
		t.Fatal(err)
	}
	for lootRows.Next() {
		var key, category string
		if err = lootRows.Scan(&key, &category); err != nil {
			t.Fatal(err)
		}
		if category != lootExpected[key] {
			t.Errorf("loot case %s classified as %q, want %q", key, category, lootExpected[key])
		}
		delete(lootExpected, key)
	}
	if err = lootRows.Err(); err != nil {
		t.Fatal(err)
	}
	lootRows.Close()
	if len(lootExpected) != 0 {
		t.Fatalf("loot classification cases were not evaluated: %#v", lootExpected)
	}
}
