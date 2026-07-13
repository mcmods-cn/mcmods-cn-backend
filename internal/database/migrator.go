package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type migration struct {
	version    int
	name       string
	statements []string
}

func Migrate(ctx context.Context, db *pgxpool.Pool) error {
	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err = conn.Exec(ctx, `select pg_advisory_lock(hashtext('mcmods-cn-schema-migrations'))`); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer conn.Exec(context.Background(), `select pg_advisory_unlock(hashtext('mcmods-cn-schema-migrations'))`)

	if _, err = conn.Exec(ctx, `create table if not exists schema_migrations (
		version integer primary key,
		name text not null,
		applied_at timestamptz not null default now()
	)`); err != nil {
		return fmt.Errorf("create schema migration table: %w", err)
	}

	migrations := []migration{
		{version: 1, name: "initial schema baseline", statements: baselineSchemaStatements()},
		{version: 2, name: "immutable content review history", statements: immutableReviewSchemaStatements()},
		{version: 3, name: "protect append-only history", statements: immutableHistoryGuardStatements()},
		{version: 4, name: "protect permission audit history", statements: []string{
			`drop trigger if exists trg_permission_audit_logs_immutable on permission_audit_logs`,
			`create trigger trg_permission_audit_logs_immutable before update or delete on permission_audit_logs
			 for each row execute function prevent_immutable_history_mutation()`,
		}},
		{version: 5, name: "global tags and recipe catalogs", statements: globalCatalogSchemaStatements()},
		{version: 6, name: "authoritative recipe identities", statements: authoritativeRecipeIdentityStatements()},
		{version: 7, name: "indexed recipe item associations", statements: recipeItemAssociationStatements()},
		{version: 8, name: "compact recipe layouts", statements: compactRecipeLayoutStatements()},
		{version: 9, name: "precomputed export revision data", statements: exportDerivedDataStatements()},
		{version: 10, name: "export capability snapshots", statements: exportCapabilityStatements()},
		{version: 11, name: "typed recipe ingredient associations", statements: typedRecipeIngredientStatements()},
	}
	for _, item := range migrations {
		var applied bool
		if err = conn.QueryRow(ctx, `select exists(select 1 from schema_migrations where version=$1)`, item.version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %d: %w", item.version, err)
		}
		if applied {
			continue
		}

		tx, beginErr := conn.Begin(ctx)
		if beginErr != nil {
			return fmt.Errorf("begin migration %d: %w", item.version, beginErr)
		}
		for _, statement := range item.statements {
			if _, err = tx.Exec(ctx, statement); err != nil {
				_ = tx.Rollback(ctx)
				return fmt.Errorf("apply migration %d (%s): %w", item.version, item.name, err)
			}
		}
		if _, err = tx.Exec(ctx, `insert into schema_migrations(version,name) values($1,$2)`, item.version, item.name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %d: %w", item.version, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %d: %w", item.version, err)
		}
	}
	return nil
}

func typedRecipeIngredientStatements() []string {
	return []string{
		`alter table mod_export_recipe_items add column if not exists ingredient_kind text not null default 'item'`,
		`alter table mod_export_recipe_items add column if not exists ingredient_type text not null default 'item_stack'`,
		`alter table mod_export_recipe_items add column if not exists unique_id text not null default ''`,
		`alter table mod_export_recipe_items add column if not exists nbt_snbt text not null default ''`,
		`create index if not exists idx_mod_export_recipe_items_ingredient
			on mod_export_recipe_items(revision_id,ingredient_kind,item_id,role,recipe_key)`,
		`create index if not exists idx_mod_export_recipe_items_unique
			on mod_export_recipe_items(unique_id) where unique_id<>''`,
	}
}

func exportCapabilityStatements() []string {
	return []string{
		`create table if not exists mod_export_capabilities (
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			capability_id text not null,
			status text not null,
			source text not null default '',
			data jsonb not null default '{}'::jsonb,
			primary key(revision_id,capability_id),
			check(status in ('available','degraded','unavailable'))
		)`,
		`create index if not exists idx_mod_export_capabilities_status
			on mod_export_capabilities(revision_id,status,capability_id)`,
		`alter table mod_export_revision_stats add column if not exists capability_statuses jsonb not null default '{}'::jsonb`,
		`insert into mod_export_capabilities(revision_id,capability_id,status,source,data)
		 select asset.revision_id,capability.value->>'id',capability.value->>'status',
			coalesce(capability.value->>'source',''),capability.value
		 from mod_export_text_assets asset
		 cross join lateral jsonb_array_elements(coalesce(asset.json_content->'capabilities','[]'::jsonb)) capability(value)
		 where asset.asset_path='compatibility/capabilities.json'
		   and capability.value->>'id'<>''
		   and capability.value->>'status' in ('available','degraded','unavailable')
		 on conflict(revision_id,capability_id) do update set
			status=excluded.status,source=excluded.source,data=excluded.data`,
		`update mod_export_revision_stats stats set capability_statuses=coalesce((
			select jsonb_object_agg(capability_id,jsonb_build_object('status',status,'source',source))
			from mod_export_capabilities where revision_id=stats.revision_id
		),'{}'::jsonb)`,
	}
}

func exportDerivedDataStatements() []string {
	return []string{
		`alter table mod_export_recipe_items add column if not exists tag_id text not null default ''`,
		`update mod_export_recipe_items item set tag_id=coalesce(slot.value->>'item_tag_equivalent',slot.value->>'tag','')
		 from mod_export_recipe_layouts layout
		 cross join lateral jsonb_array_elements(coalesce(layout.layout->'slots','[]'::jsonb)) with ordinality slot(value,ordinality)
		 where item.revision_id=layout.revision_id and item.recipe_key=layout.recipe_key
		   and item.slot_index=(slot.ordinality-1)::integer and item.tag_id=''
		   and coalesce(slot.value->>'item_tag_equivalent',slot.value->>'tag','')<>''`,
		`create index if not exists idx_mod_export_recipe_items_tag
		 on mod_export_recipe_items(tag_id) where tag_id<>''`,
		`create table if not exists mod_export_document_entries (
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			kind text not null,
			entry_id text not null,
			ordinal integer not null,
			namespace text not null default '',
			names jsonb not null default '{}'::jsonb,
			icon_path text not null default '',
			preview_path text not null default '',
			data jsonb not null default '{}'::jsonb,
			primary key(revision_id,kind,ordinal)
		)`,
		`create index if not exists idx_mod_export_document_entries_lookup
		 on mod_export_document_entries(revision_id,kind,entry_id)`,
		`create table if not exists mod_export_revision_stats (
			revision_id text primary key references mod_export_revisions(id) on delete cascade,
			registry_counts jsonb not null default '{}'::jsonb,
			document_counts jsonb not null default '{}'::jsonb,
			asset_count integer not null default 0,
			structure_count integer not null default 0,
			advancement_count integer not null default 0,
			key_mapping_count integer not null default 0,
			recipe_count integer not null default 0,
			tag_count integer not null default 0,
			updated_at timestamptz not null default now()
		)`,
		`insert into mod_export_revision_stats(
			revision_id,registry_counts,document_counts,asset_count,structure_count,
			advancement_count,key_mapping_count,recipe_count,tag_count
		)
		select revision.id,
			coalesce((select jsonb_object_agg(registry,total) from (
				select registry,count(*)::int total from mod_export_registry_entries
				where revision_id=revision.id group by registry
			) counts),'{}'::jsonb),
			jsonb_build_object(
				'biomes',coalesce((select jsonb_array_length(json_content->'biomes') from mod_export_text_assets where revision_id=revision.id and asset_path='worldgen/biomes.json'),0),
				'dimensions',coalesce((select jsonb_array_length(json_content->'dimensions') from mod_export_text_assets where revision_id=revision.id and asset_path='worldgen/dimensions.json'),0),
				'natural_generation',coalesce((select (json_content->>'entry_count')::int from mod_export_text_assets where revision_id=revision.id and asset_path='worldgen/natural_generation.json'),0),
				'world_structures',coalesce((select jsonb_array_length(json_content->'structures') from mod_export_text_assets where revision_id=revision.id and asset_path='worldgen/structures.json'),0),
				'loot_tables',coalesce((select jsonb_array_length(json_content->'loot_tables') from mod_export_text_assets where revision_id=revision.id and asset_path='worldgen/loot_tables.json'),0),
				'ingredients',coalesce((select (json_content->>'ingredient_count')::int from mod_export_text_assets where revision_id=revision.id and asset_path='ingredients/ingredients.json'),0),
				'worldgen_data',coalesce((select jsonb_array_length(json_content->'files') from mod_export_text_assets where revision_id=revision.id and asset_path='worldgen/data_files.json'),0)
			),
			(select count(*)::int from (
				select asset_path from mod_export_text_assets where revision_id=revision.id
				union all select asset_path from mod_export_binary_assets where revision_id=revision.id
				union all select asset_path from mod_export_media where revision_id=revision.id
			) assets),
			(select count(*)::int from mod_export_structures where revision_id=revision.id),
			coalesce((select jsonb_array_length(json_content->'advancements') from mod_export_text_assets where revision_id=revision.id and asset_path='advancements/advancements.json'),0),
			(select count(*)::int from mod_export_registry_entries where revision_id=revision.id and registry='key_mappings'),
			(select count(*)::int from mod_export_recipe_layouts where revision_id=revision.id),
			(select count(*)::int from mod_export_tags where revision_id=revision.id)
		from mod_export_revisions revision
		on conflict(revision_id) do nothing`,
	}
}

func compactRecipeLayoutStatements() []string {
	return []string{
		`alter table mod_export_recipe_layouts add column if not exists compact_layout jsonb not null default '{}'::jsonb`,
		`update mod_export_recipe_layouts layout_row set compact_layout=(layout_row.layout-'slots') || jsonb_build_object('slots',coalesce((
			select jsonb_agg((slot.value-'alternatives') || jsonb_build_object('alternatives',coalesce((
				select jsonb_agg(alternative.value-'names'-'title_names'-'translation_key'-'title_translation_key' order by alternative.ordinality)
				from jsonb_array_elements(coalesce(slot.value->'alternatives','[]'::jsonb)) with ordinality alternative(value,ordinality)
			),'[]'::jsonb)) order by slot.ordinality)
			from jsonb_array_elements(coalesce(layout_row.layout->'slots','[]'::jsonb)) with ordinality slot(value,ordinality)
		),'[]'::jsonb)) where compact_layout='{}'::jsonb`,
	}
}

func recipeItemAssociationStatements() []string {
	return []string{
		`create table if not exists mod_export_recipe_items (
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			recipe_key text not null,
			recipe_type_id text not null,
			recipe_id text not null,
			role text not null,
			slot_index integer not null,
			alternative_index integer not null,
			item_id text not null,
			amount double precision not null default 1,
			primary key(revision_id,recipe_key,role,slot_index,alternative_index,item_id),
			check(role in ('input','output','catalyst'))
		)`,
		`create index if not exists idx_mod_export_recipe_items_lookup
		 on mod_export_recipe_items(revision_id,item_id,role,recipe_key)`,
		`create index if not exists idx_mod_export_recipe_items_recipe
		 on mod_export_recipe_items(revision_id,recipe_key)`,
		`insert into mod_export_recipe_items(revision_id,recipe_key,recipe_type_id,recipe_id,role,slot_index,alternative_index,item_id,amount)
		 select layout.revision_id,layout.recipe_key,layout.recipe_type_id,layout.recipe_id,slot.value->>'role',
			(slot.ordinality-1)::integer,(alternative.ordinality-1)::integer,
			coalesce(alternative.value->>'item',alternative.value->>'resource_location'),
			case when jsonb_typeof(alternative.value->'count')='number' then (alternative.value->>'count')::double precision
				 when jsonb_typeof(alternative.value->'amount')='number' then (alternative.value->>'amount')::double precision else 1 end
		 from mod_export_recipe_layouts layout
		 cross join lateral jsonb_array_elements(coalesce(layout.layout->'slots','[]'::jsonb)) with ordinality slot(value,ordinality)
		 cross join lateral jsonb_array_elements(coalesce(slot.value->'alternatives','[]'::jsonb)) with ordinality alternative(value,ordinality)
		 where slot.value->>'role' in ('input','output','catalyst')
		   and coalesce(alternative.value->>'item',alternative.value->>'resource_location','')<>''
		 on conflict do nothing`,
	}
}

func authoritativeRecipeIdentityStatements() []string {
	return []string{
		`alter table mod_export_recipe_types add column if not exists recipes jsonb not null default '[]'::jsonb`,
		`alter table mod_export_recipe_layouts rename column fingerprint to semantic_fingerprint`,
		`alter table mod_export_recipe_layouts add column if not exists recipe_id_source text not null default 'legacy'`,
		`alter table mod_export_recipe_layouts add column if not exists recipe_id_canonical boolean not null default false`,
		`alter table mod_export_recipe_layouts add column if not exists recipe_key text not null default ''`,
		`update mod_export_recipe_layouts layout set recipe_key='package:'||revision.package_id||':'||layout.recipe_type_id||':'||layout.recipe_id
		 from mod_export_revisions revision where revision.id=layout.revision_id and layout.recipe_key=''`,
		`alter table mod_export_recipe_layouts add constraint chk_mod_export_recipe_layout_identity
		 check ((recipe_id_source in ('minecraft_recipe','jei_category') and recipe_id_canonical)
		 or (recipe_id_source in ('generated_index','legacy') and not recipe_id_canonical))`,
		`drop index if exists idx_mod_export_recipe_layouts_type`,
		`drop index if exists idx_mod_export_recipe_layouts_fingerprint`,
		`create index if not exists idx_mod_export_recipe_layouts_type_key on mod_export_recipe_layouts(recipe_type_id,recipe_key)`,
		`create index if not exists idx_mod_export_recipe_layouts_semantic on mod_export_recipe_layouts(semantic_fingerprint)`,
		`alter table global_recipe_contents rename column fingerprint to recipe_key`,
	}
}

func globalCatalogSchemaStatements() []string {
	return []string{
		`create table if not exists mod_export_recipe_types (
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			recipe_type_id text not null,
			title_translation_key text not null default '',
			title_names jsonb not null default '{}'::jsonb,
			width integer not null default 0,
			height integer not null default 0,
			background_path text not null default '',
			background_contains_ingredients boolean not null default false,
			image_scale integer not null default 1,
			canvas jsonb not null default '{}'::jsonb,
			catalysts jsonb not null default '[]'::jsonb,
			catalyst_count integer not null default 0,
			recipe_count integer not null default 0,
			primary key(revision_id,recipe_type_id)
		)`,
		`create index if not exists idx_mod_export_recipe_types_id on mod_export_recipe_types(recipe_type_id)`,
		`create table if not exists mod_export_recipe_layouts (
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			recipe_type_id text not null,
			recipe_id text not null,
			fingerprint text not null,
			layout_path text not null,
			background_path text not null default '',
			layout jsonb not null,
			primary key(revision_id,recipe_type_id,recipe_id,layout_path)
		)`,
		`create index if not exists idx_mod_export_recipe_layouts_type on mod_export_recipe_layouts(recipe_type_id,fingerprint)`,
		`create index if not exists idx_mod_export_recipe_layouts_fingerprint on mod_export_recipe_layouts(fingerprint)`,
		`create table if not exists global_tag_contents (
			registry text not null,
			tag_id text not null,
			locale text not null,
			content_markdown text not null default '',
			updated_by bigint references users(id) on delete set null,
			published_revision_id bigint references content_revisions(id) on delete restrict,
			updated_at timestamptz not null default now(),
			primary key(registry,tag_id,locale)
		)`,
		`create table if not exists global_tag_member_overrides (
			registry text not null,
			tag_id text not null,
			member_ids text[] not null default '{}'::text[],
			updated_by bigint references users(id) on delete set null,
			published_revision_id bigint references content_revisions(id) on delete restrict,
			updated_at timestamptz not null default now(),
			primary key(registry,tag_id)
		)`,
		`create table if not exists global_recipe_type_contents (
			recipe_type_id text not null,
			locale text not null,
			content_markdown text not null default '',
			updated_by bigint references users(id) on delete set null,
			published_revision_id bigint references content_revisions(id) on delete restrict,
			updated_at timestamptz not null default now(),
			primary key(recipe_type_id,locale)
		)`,
		`create table if not exists global_recipe_type_catalyst_overrides (
			recipe_type_id text primary key,
			catalysts jsonb not null default '[]'::jsonb,
			updated_by bigint references users(id) on delete set null,
			published_revision_id bigint references content_revisions(id) on delete restrict,
			updated_at timestamptz not null default now()
		)`,
		`create table if not exists global_recipe_contents (
			fingerprint text primary key,
			note text not null default '',
			layout_override jsonb,
			updated_by bigint references users(id) on delete set null,
			published_revision_id bigint references content_revisions(id) on delete restrict,
			updated_at timestamptz not null default now()
		)`,
	}
}

func ResetDevelopmentSchema(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `do $$
	declare object record;
	begin
		for object in
			select table_name name,'table' kind from information_schema.tables
			where table_schema='public' and table_type='BASE TABLE'
			union all
			select table_name name,'view' kind from information_schema.views where table_schema='public'
		loop
			execute format('drop %s if exists public.%I cascade',object.kind,object.name);
		end loop;
		for object in select sequence_name name from information_schema.sequences where sequence_schema='public'
		loop
			execute format('drop sequence if exists public.%I cascade',object.name);
		end loop;
	end $$`); err != nil {
		return fmt.Errorf("drop development database objects: %w", err)
	}
	return tx.Commit(ctx)
}

func immutableReviewSchemaStatements() []string {
	return []string{
		`create table if not exists content_revisions (
			id bigserial primary key,
			aggregate_type text not null,
			aggregate_key text not null,
			revision_no bigint not null,
			base_revision_id bigint references content_revisions(id) on delete restrict,
			schema_version integer not null default 1,
			snapshot jsonb not null,
			snapshot_hash text not null,
			created_by bigint references users(id) on delete set null,
			created_by_snapshot text not null default '',
			source text not null default 'user',
			created_at timestamptz not null default now(),
			unique(aggregate_type,aggregate_key,revision_no)
		)`,
		`create index if not exists idx_content_revisions_aggregate on content_revisions(aggregate_type,aggregate_key,revision_no desc)`,
		`create table if not exists change_requests (
			id bigserial primary key,
			aggregate_type text not null,
			aggregate_key text not null,
			base_revision_id bigint references content_revisions(id) on delete restrict,
			proposed_revision_id bigint not null unique references content_revisions(id) on delete restrict,
			status text not null default 'pending',
			reason text not null default '',
			submitted_by bigint references users(id) on delete set null,
			submitted_by_snapshot text not null default '',
			metadata jsonb not null default '{}'::jsonb,
			submitted_at timestamptz not null default now(),
			resolved_at timestamptz,
			check(status in ('pending','approved','rejected','conflicted','withdrawn'))
		)`,
		`create index if not exists idx_change_requests_queue on change_requests(status,submitted_at,id)`,
		`create index if not exists idx_change_requests_aggregate on change_requests(aggregate_type,aggregate_key,submitted_at desc)`,
		`create table if not exists review_events (
			id bigserial primary key,
			change_request_id bigint not null references change_requests(id) on delete restrict,
			event_type text not null,
			actor_id bigint references users(id) on delete set null,
			actor_snapshot text not null default '',
			note text not null default '',
			ip text not null default '',
			user_agent text not null default '',
			metadata jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now(),
			check(event_type in ('submitted','approved','rejected','conflicted','withdrawn','published'))
		)`,
		`create index if not exists idx_review_events_request_created on review_events(change_request_id,created_at,id)`,
		`create table if not exists content_change_items (
			id bigserial primary key,
			revision_id bigint not null references content_revisions(id) on delete restrict,
			path text not null,
			operation text not null,
			before_value jsonb,
			after_value jsonb,
			check(operation in ('add','remove','replace'))
		)`,
		`create index if not exists idx_content_change_items_revision on content_change_items(revision_id,id)`,
		`create table if not exists audit_events (
			id bigserial primary key,
			aggregate_type text not null,
			aggregate_key text not null,
			actor_id bigint references users(id) on delete set null,
			actor_snapshot text not null default '',
			action text not null,
			before_hash text not null default '',
			after_hash text not null default '',
			trace_id text not null default '',
			ip text not null default '',
			user_agent text not null default '',
			metadata jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now()
		)`,
		`create index if not exists idx_audit_events_aggregate on audit_events(aggregate_type,aggregate_key,created_at desc,id desc)`,
		`alter table mod_export_entry_contents add column if not exists published_revision_id bigint references content_revisions(id) on delete restrict`,
		`alter table users add column if not exists profile_revision_id bigint references content_revisions(id) on delete restrict`,
		`insert into content_revisions(id,aggregate_type,aggregate_key,revision_no,schema_version,snapshot,snapshot_hash,created_by,created_by_snapshot,source,created_at)
		 select r.id,'mod',r.mod_id::text,r.version,1,r.snapshot,md5(r.snapshot::text),r.submitted_by,coalesce(u.username,''),'legacy',r.created_at
		 from mod_revisions r left join users u on u.id=r.submitted_by on conflict(id) do nothing`,
		`with previous as (
			select id,lag(id) over(partition by aggregate_type,aggregate_key order by revision_no) previous_id from content_revisions
		) update content_revisions r set base_revision_id=previous.previous_id from previous where previous.id=r.id and r.base_revision_id is null`,
		`insert into change_requests(aggregate_type,aggregate_key,base_revision_id,proposed_revision_id,status,reason,submitted_by,submitted_by_snapshot,submitted_at,resolved_at)
		 select 'mod',r.mod_id::text,c.base_revision_id,c.id,r.status,r.change_reason,r.submitted_by,coalesce(u.username,''),r.created_at,r.reviewed_at
		 from mod_revisions r join content_revisions c on c.id=r.id left join users u on u.id=r.submitted_by
		 on conflict(proposed_revision_id) do nothing`,
		`insert into review_events(change_request_id,event_type,actor_id,actor_snapshot,note,created_at)
		 select q.id,'submitted',q.submitted_by,q.submitted_by_snapshot,'',q.submitted_at from change_requests q
		 where not exists(select 1 from review_events e where e.change_request_id=q.id and e.event_type='submitted')`,
		`insert into review_events(change_request_id,event_type,actor_id,actor_snapshot,note,created_at)
		 select q.id,q.status,r.reviewed_by,coalesce(u.username,''),r.review_note,coalesce(r.reviewed_at,q.resolved_at,now())
		 from change_requests q join mod_revisions r on r.id=q.proposed_revision_id left join users u on u.id=r.reviewed_by
		 where q.status in ('approved','rejected') and not exists(
			select 1 from review_events e where e.change_request_id=q.id and e.event_type=q.status
		 )`,
		`alter table mods add column if not exists published_revision_id bigint references content_revisions(id) on delete restrict`,
		`do $$ begin
			if exists(select 1 from information_schema.columns where table_schema='public' and table_name='mods' and column_name='current_revision_id') then
				execute 'update mods set published_revision_id=current_revision_id where current_revision_id is not null';
			end if;
		end $$`,
		`select setval(pg_get_serial_sequence('content_revisions','id'),greatest(coalesce((select max(id) from content_revisions),1),1),true)`,
		`alter table mods drop column if exists current_revision_id`,
		`drop table if exists mod_revisions`,
	}
}

func immutableHistoryGuardStatements() []string {
	return []string{
		`create or replace function prevent_immutable_history_mutation() returns trigger as $$
		begin
			raise exception '% is append-only',tg_table_name;
		end;
		$$ language plpgsql`,
		`drop trigger if exists trg_content_revisions_immutable on content_revisions`,
		`create trigger trg_content_revisions_immutable before update or delete on content_revisions
		 for each row execute function prevent_immutable_history_mutation()`,
		`drop trigger if exists trg_review_events_immutable on review_events`,
		`create trigger trg_review_events_immutable before update or delete on review_events
		 for each row execute function prevent_immutable_history_mutation()`,
		`drop trigger if exists trg_content_change_items_immutable on content_change_items`,
		`create trigger trg_content_change_items_immutable before update or delete on content_change_items
		 for each row execute function prevent_immutable_history_mutation()`,
		`drop trigger if exists trg_audit_events_immutable on audit_events`,
		`create trigger trg_audit_events_immutable before update or delete on audit_events
		 for each row execute function prevent_immutable_history_mutation()`,
	}
}
