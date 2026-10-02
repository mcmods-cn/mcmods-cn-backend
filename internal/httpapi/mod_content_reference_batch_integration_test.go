package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

type modContentReferenceSyncQueryCounter struct {
	active atomic.Bool
	count  atomic.Int64
}

func (counter *modContentReferenceSyncQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	if counter.active.Load() {
		counter.count.Add(1)
	}
	return ctx
}

func (*modContentReferenceSyncQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestModContentReferenceSyncUsesConstantQueriesAtMaximumFieldScaleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify batched mod-content reference synchronization")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	counter := &modContentReferenceSyncQueryCounter{}
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	poolConfig.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var modID, versionID, templateID, sectionID, sourceResourceID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values('refbatch1','reference-batch','Reference batch','approved') returning id`).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,mod_version,status)
		values($1,'1.21.1 / NeoForge',array['1.21.1'],array['neoforge'],'1.0.0','active') returning id`, modID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	values := make([]any, 500)
	for index := range values {
		values[index] = fmt.Sprintf("batch:value_%03d", index+1)
	}
	definition := map[string]any{
		"resourceKinds": []any{"minecraft.item"},
		"entryTypes": []any{map[string]any{
			"code": "default",
			"groups": []any{map[string]any{
				"code": "references",
				"fields": []any{
					map[string]any{"code": "tagRefsOne", "type": "reference-list", "referenceKind": "tag", "referenceRegistry": "minecraft:item"},
					map[string]any{"code": "tagRefsTwo", "type": "reference-list", "referenceKind": "tag", "referenceRegistry": "minecraft:item"},
					map[string]any{"code": "itemRefsOne", "type": "reference-list", "referenceKind": "minecraft.item"},
					map[string]any{"code": "itemRefsTwo", "type": "reference-list", "referenceKind": "minecraft.item"},
				},
			}},
		}},
	}
	templateJSON, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_templates(owner_mod_id,code,definition)
		values($1,'reference_batch',$2::jsonb) returning id`, modID, templateJSON).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_sections(mod_id,version_id,template_id,default_locale,display_mode,status)
		values($1,$2,$3,'en-US','compact','active') returning id`, modID, versionID, templateID).Scan(&sectionID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type,status)
		values('reference-batch-source','resource','active') returning id`).Scan(&sourceResourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved)
		values($1,'minecraft.item','batch:source','batch','source',$2,true)`, sourceResourceID, modID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_resource_bindings(resource_id,mod_id) values($1,$2)`, sourceResourceID, modID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_resource_version_details(resource_id,version_id,entry_type_code,definition,status)
		values($1,$2,'default','{}','active')`, sourceResourceID, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_content_section_resources(section_id,version_id,resource_id,ordinal)
		values($1,$2,$3,0)`, sectionID, versionID, sourceResourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into catalog_entities(identity_key,entity_type,status)
		select 'reference-batch-tag-'||value,'tag','active' from generate_series(1,250) value;
		insert into catalog_tags(entity_id,registry,canonical_id)
		select entity.id,'minecraft:item','batch:value_'||lpad(split_part(entity.identity_key,'-',4),3,'0')
		from catalog_entities entity where entity.identity_key like 'reference-batch-tag-%';
		insert into catalog_entities(identity_key,entity_type,status)
		select 'reference-batch-resource-'||value,'resource','active' from generate_series(1,250) value;
		insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,resolved)
		select entity.id,'minecraft.item',
			case when split_part(entity.identity_key,'-',4)::int<=125
				then 'batch:value_'||lpad(split_part(entity.identity_key,'-',4),3,'0')
				else 'batch:resolved_'||lpad(split_part(entity.identity_key,'-',4),3,'0') end,
			'batch','resolved_'||lpad(split_part(entity.identity_key,'-',4),3,'0'),true
		from catalog_entities entity where entity.identity_key like 'reference-batch-resource-%';
		insert into game_resource_aliases(kind_code,alias_id,resource_id)
		select 'minecraft.item','batch:value_'||lpad(split_part(entity.identity_key,'-',4),3,'0'),entity.id
		from catalog_entities entity
		where entity.identity_key like 'reference-batch-resource-%'
		  and split_part(entity.identity_key,'-',4)::int>125`); err != nil {
		t.Fatal(err)
	}

	resourceDefinition := map[string]any{
		"tagRefsOne":  values,
		"tagRefsTwo":  values,
		"itemRefsOne": values,
		"itemRefsTwo": values,
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counter.count.Store(0)
	counter.active.Store(true)
	syncStarted := time.Now()
	err = syncModContentResourceUnresolvedReferencesTx(ctx, tx, sourceResourceID, versionID, resourceDefinition)
	syncDuration := time.Since(syncStarted)
	counter.active.Store(false)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if got := counter.count.Load(); got != 5 {
		t.Fatalf("reference synchronization used %d SQL statements for 2,000 identifiers; want 5", got)
	}
	t.Logf("batched 2,000 identifiers with 5 SQL statements in %s", syncDuration)

	var unresolvedTags, unresolvedResources int
	if err = pool.QueryRow(ctx, `select count(*) from unresolved_references
		where source_type='mod_content_resource' and source_id=$1 and reference_type='tag'`, sourceResourceID).Scan(&unresolvedTags); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from unresolved_resource_references
		where source_entity_id=$1 and source_revision_id is null`, sourceResourceID).Scan(&unresolvedResources); err != nil {
		t.Fatal(err)
	}
	if unresolvedTags != 500 || unresolvedResources != 500 {
		t.Fatalf("unresolved counts tags=%d resources=%d; want 500/500", unresolvedTags, unresolvedResources)
	}
	for _, fieldCode := range []string{"tagRefsOne", "tagRefsTwo", "itemRefsOne", "itemRefsTwo"} {
		var count int
		table := "unresolved_resource_references"
		if strings.HasPrefix(fieldCode, "tag") {
			table = "unresolved_references"
		}
		query := fmt.Sprintf(`select count(*) from %s where field_path like $1`, table)
		if err = pool.QueryRow(ctx, query, fmt.Sprintf("version.%d.%s.%%", versionID, fieldCode)).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 250 {
			t.Errorf("%s unresolved paths=%d; want 250", fieldCode, count)
		}
	}

	rollbackTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counter.count.Store(0)
	counter.active.Store(true)
	err = syncModContentResourceUnresolvedReferencesTx(ctx, rollbackTx, sourceResourceID, versionID, map[string]any{})
	counter.active.Store(false)
	if err != nil {
		_ = rollbackTx.Rollback(ctx)
		t.Fatal(err)
	}
	if got := counter.count.Load(); got != 3 {
		_ = rollbackTx.Rollback(ctx)
		t.Fatalf("empty replacement used %d SQL statements; want 3", got)
	}
	if err = rollbackTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select
		(select count(*) from unresolved_references where source_type='mod_content_resource' and source_id=$1),
		(select count(*) from unresolved_resource_references where source_entity_id=$1 and source_revision_id is null)`,
		sourceResourceID).Scan(&unresolvedTags, &unresolvedResources); err != nil {
		t.Fatal(err)
	}
	if unresolvedTags != 500 || unresolvedResources != 500 {
		t.Fatalf("rolled-back replacement changed unresolved facts: tags=%d resources=%d", unresolvedTags, unresolvedResources)
	}

	scaleTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = scaleTx.Rollback(context.Background()) }()
	if _, err = scaleTx.Exec(ctx, `create temporary table mod_content_reference_scale_entities(
		id bigint primary key,status text not null
	);
	insert into mod_content_reference_scale_entities
	select value,'active' from generate_series(1,1000000) value;
	create temporary table mod_content_reference_scale_tags(
		entity_id bigint primary key,registry text not null,canonical_id text not null
	);
	insert into mod_content_reference_scale_tags
	select value,'minecraft:item','batch:tag_'||lpad(value::text,7,'0') from generate_series(1,1000000) value;
	create index mod_content_reference_scale_tags_folded
		on mod_content_reference_scale_tags(lower(canonical_id),lower(registry),entity_id);
	create temporary table mod_content_reference_scale_resources(
		entity_id bigint primary key,kind_code text not null,canonical_id text not null
	);
	insert into mod_content_reference_scale_resources
	select value,'minecraft.item','batch:item_'||lpad(value::text,7,'0') from generate_series(1,1000000) value;
	create index mod_content_reference_scale_resources_folded
		on mod_content_reference_scale_resources(kind_code,lower(canonical_id),entity_id);
	create temporary table mod_content_reference_scale_aliases(
		kind_code text not null,alias_id text not null,resource_id bigint not null
	);
	insert into mod_content_reference_scale_aliases
	select 'minecraft.item','batch:alias_'||lpad(value::text,7,'0'),value from generate_series(1,1000000) value;
	create index mod_content_reference_scale_aliases_folded
		on mod_content_reference_scale_aliases(kind_code,lower(alias_id),resource_id);
	create temporary table mod_content_reference_scale_unresolved_tags(
		source_type text not null,source_id bigint not null,field_path text not null,reference_type text not null,
		raw_identifier text not null,normalized_identifier text not null,resolved_type text not null default '',
		resolved_id bigint,status text not null default 'pending',metadata jsonb not null default '{}',
		resolved_at timestamptz,updated_at timestamptz not null default now(),
		unique(source_type,source_id,field_path,reference_type,normalized_identifier)
	);
	create temporary table mod_content_reference_scale_unresolved_resources(
		source_entity_id bigint not null,field_path text not null,kind_code text not null,
		raw_resource_id text not null,status text not null
	);
	analyze mod_content_reference_scale_entities;
	analyze mod_content_reference_scale_tags;
	analyze mod_content_reference_scale_resources;
	analyze mod_content_reference_scale_aliases`); err != nil {
		t.Fatal(err)
	}

	tagPaths := make([]string, 500)
	tagRaw := make([]string, 500)
	tagNormalized := make([]string, 500)
	tagRegistry := make([]string, 500)
	resourcePaths := make([]string, 500)
	resourceKinds := make([]string, 500)
	resourceRaw := make([]string, 500)
	resourceNormalized := make([]string, 500)
	for index := 0; index < 500; index++ {
		tagValue := fmt.Sprintf("batch:tag_%07d", 999501+index)
		tagPaths[index] = fmt.Sprintf("version.1.tags.%d", index)
		tagRaw[index] = tagValue
		tagNormalized[index] = tagValue
		tagRegistry[index] = "minecraft:item"
		resourcePaths[index] = fmt.Sprintf("version.1.items.%d", index)
		resourceKinds[index] = "minecraft.item"
		if index < 250 {
			resourceRaw[index] = fmt.Sprintf("batch:item_%07d", 999751+index)
		} else {
			resourceRaw[index] = fmt.Sprintf("batch:alias_%07d", 999501+index)
		}
		resourceNormalized[index] = resourceRaw[index]
	}
	tagScaleSQL := modContentReferenceScaleSQL(modContentTagReferenceBatchSQL)
	resourceScaleSQL := modContentReferenceScaleSQL(modContentResourceReferenceBatchSQL)
	tagPlan, tagElapsed := explainModContentReferenceScalePlan(t, ctx, scaleTx, tagScaleSQL,
		int64(1), int64(1), tagPaths, tagRaw, tagNormalized, tagRegistry)
	resourcePlan, resourceElapsed := explainModContentReferenceScalePlan(t, ctx, scaleTx, resourceScaleSQL,
		int64(1), resourcePaths, resourceKinds, resourceRaw, resourceNormalized)
	if !strings.Contains(tagPlan, "mod_content_reference_scale_tags_folded") ||
		strings.Contains(tagPlan, "Seq Scan on mod_content_reference_scale_tags") {
		t.Fatalf("1m tag resolution missed its folded index:\n%s", tagPlan)
	}
	if !strings.Contains(resourcePlan, "mod_content_reference_scale_resources_folded") ||
		!strings.Contains(resourcePlan, "mod_content_reference_scale_aliases_folded") ||
		strings.Contains(resourcePlan, "Seq Scan on mod_content_reference_scale_resources") ||
		strings.Contains(resourcePlan, "Seq Scan on mod_content_reference_scale_aliases") {
		t.Fatalf("1m resource resolution missed canonical or alias folded index:\n%s", resourcePlan)
	}
	if tagElapsed > 2*time.Second || resourceElapsed > 2*time.Second {
		t.Fatalf("1m reference resolution exceeded budget: tags=%s resources=%s", tagElapsed, resourceElapsed)
	}
	t.Logf("1m folded lookup plans: tags=%s resources=%s\ntag plan:\n%s\nresource plan:\n%s",
		tagElapsed, resourceElapsed, tagPlan, resourcePlan)
}

func modContentReferenceScaleSQL(query string) string {
	return strings.NewReplacer(
		"unresolved_resource_references", "mod_content_reference_scale_unresolved_resources",
		"unresolved_references", "mod_content_reference_scale_unresolved_tags",
		"game_resource_aliases", "mod_content_reference_scale_aliases",
		"game_resources", "mod_content_reference_scale_resources",
		"catalog_tags", "mod_content_reference_scale_tags",
		"catalog_entities", "mod_content_reference_scale_entities",
	).Replace(query)
}

func explainModContentReferenceScalePlan(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	query string,
	args ...any,
) (string, time.Duration) {
	t.Helper()
	started := time.Now()
	rows, err := tx.Query(ctx, "explain (analyze,buffers,format text) "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return plan.String(), time.Since(started)
}
