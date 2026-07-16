package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const schemaGeneration = 5

// Migrate installs one coherent development schema. The catalog redesign does
// not support in-place upgrades from the pre-entity data model; development
// databases must be reset before this generation is installed.
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

	var hasSchemaMetadata bool
	if err = conn.QueryRow(ctx, `select to_regclass('public.schema_metadata') is not null`).Scan(&hasSchemaMetadata); err != nil {
		return fmt.Errorf("inspect schema generation: %w", err)
	}
	currentGeneration := 0
	if hasSchemaMetadata {
		err = conn.QueryRow(ctx, `select coalesce((select generation from schema_metadata where singleton),0)`).Scan(&currentGeneration)
	}
	if err != nil {
		return fmt.Errorf("read schema generation: %w", err)
	}
	if currentGeneration == 3 {
		if err = upgradeSchema3To4(ctx, conn); err != nil {
			return err
		}
		currentGeneration = 4
	}
	if currentGeneration == 4 {
		if err = upgradeSchema4To5(ctx, conn); err != nil {
			return err
		}
		return nil
	}
	if currentGeneration != 0 && currentGeneration != schemaGeneration {
		return fmt.Errorf("database schema generation %d is incompatible with generation %d; reset the development database", currentGeneration, schemaGeneration)
	}
	if currentGeneration == schemaGeneration {
		return nil
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin schema installation: %w", err)
	}
	defer tx.Rollback(ctx)
	statements := make([]string, 0, 256)
	statements = append(statements, baselineSchemaStatements()...)
	statements = append(statements, catalogSchemaStatements()...)
	statements = append(statements, reviewSchemaStatements()...)
	statements = append(statements, immutableHistoryGuardStatements()...)
	statements = append(statements, schemaGeneration5Statements()...)
	for _, statement := range statements {
		if _, err = tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("install schema generation %d: %w", schemaGeneration, err)
		}
	}
	if _, err = tx.Exec(ctx, `create table schema_metadata (
		singleton boolean primary key default true check(singleton),
		generation integer not null,
		installed_at timestamptz not null default now()
	)`); err != nil {
		return fmt.Errorf("create schema metadata: %w", err)
	}
	if _, err = tx.Exec(ctx, `insert into schema_metadata(singleton,generation) values(true,$1)`, schemaGeneration); err != nil {
		return fmt.Errorf("record schema generation: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit schema generation: %w", err)
	}
	return nil
}

func upgradeSchema4To5(ctx context.Context, conn *pgxpool.Conn) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin schema generation 5 upgrade: %w", err)
	}
	defer tx.Rollback(ctx)
	for _, statement := range schemaGeneration5Statements() {
		if _, err = tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("upgrade schema generation 4 to 5: %w", err)
		}
	}
	if _, err = tx.Exec(ctx, `update schema_metadata set generation=$1,installed_at=now() where singleton`, schemaGeneration); err != nil {
		return fmt.Errorf("record schema generation 5: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit schema generation 5: %w", err)
	}
	return nil
}

func schemaGeneration5Statements() []string {
	return []string{
		`create table if not exists blueprint_mods (
			blueprint_id bigint not null references blueprints(id) on delete cascade,
			source_namespace text not null,
			mod_id bigint not null references mods(id) on delete cascade,
			created_at timestamptz not null default now(),
			primary key (blueprint_id, source_namespace)
		)`,
		`create index if not exists idx_blueprint_mods_mod_blueprint on blueprint_mods(mod_id, blueprint_id)`,
		`insert into blueprint_mods(blueprint_id,source_namespace,mod_id)
		 select namespace.blueprint_id,namespace.source_namespace,source.mod_id
		 from (
			select distinct blueprint_id,split_part(block_id,':',1) source_namespace
			from blueprint_materials where position(':' in block_id) > 1
		 ) namespace
		 join lateral (
			select candidate.mod_id from (
				select revision.mod_id,0 priority,coalesce(revision.activated_at,revision.created_at) matched_at
				from mod_export_revisions revision
				where revision.source_namespace=namespace.source_namespace
				  and revision.is_active and revision.status in ('ready','partial')
				union all
				select mod.id,1 priority,mod.updated_at matched_at from mods mod
				where lower(mod.mod_id)=lower(namespace.source_namespace) and mod.review_status='approved'
			) candidate order by candidate.priority,candidate.matched_at desc limit 1
		 ) source on true
		 on conflict(blueprint_id,source_namespace) do update set mod_id=excluded.mod_id`,
	}
}

func upgradeSchema3To4(ctx context.Context, conn *pgxpool.Conn) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin schema generation 4 upgrade: %w", err)
	}
	defer tx.Rollback(ctx)
	for _, statement := range schemaGeneration4Statements() {
		if _, err = tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("upgrade schema generation 3 to 4: %w", err)
		}
	}
	if _, err = tx.Exec(ctx, `update schema_metadata set generation=4,installed_at=now() where singleton`); err != nil {
		return fmt.Errorf("record schema generation 4: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit schema generation 4: %w", err)
	}
	return nil
}

func schemaGeneration4Statements() []string {
	return []string{
		`alter table blueprints add column if not exists cover_file_id bigint references oss_files(id) on delete set null`,
		`alter table blueprints add column if not exists cover_object_key text not null default ''`,
		`alter table blueprints add column if not exists review_status text not null default 'not_required'`,
		`alter table blueprints add column if not exists published_revision_id bigint references content_revisions(id) on delete restrict`,
		`do $$ begin
			alter table blueprints add constraint chk_blueprints_review_status
			check (review_status in ('not_required','pending','approved','rejected'));
		exception when duplicate_object then null; end $$`,
		`create table if not exists favorite_collections (
			id bigserial primary key,
			user_id bigint not null references users(id) on delete cascade,
			name text not null,
			is_default boolean not null default false,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			unique(user_id,name)
		)`,
		`create unique index if not exists idx_favorite_collections_default
			on favorite_collections(user_id) where is_default`,
		`create table if not exists favorite_collection_items (
			collection_id bigint not null references favorite_collections(id) on delete cascade,
			entity_type text not null,
			entity_key text not null,
			created_at timestamptz not null default now(),
			primary key(collection_id,entity_type,entity_key)
		)`,
		`create index if not exists idx_favorite_items_entity
			on favorite_collection_items(entity_type,entity_key)`,
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

func reviewSchemaStatements() []string {
	return []string{
		`create table content_revisions (
			id bigserial primary key,
			entity_id text references catalog_entities(id) on delete restrict,
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
		`create index idx_content_revisions_entity on content_revisions(entity_id,revision_no desc) where entity_id is not null`,
		`create index idx_content_revisions_aggregate on content_revisions(aggregate_type,aggregate_key,revision_no desc)`,
		`create table change_requests (
			id bigserial primary key,
			entity_id text references catalog_entities(id) on delete restrict,
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
		`create index idx_change_requests_queue on change_requests(status,submitted_at,id)`,
		`create table review_events (
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
		`create index idx_review_events_request_created on review_events(change_request_id,created_at,id)`,
		`create table content_change_items (
			id bigserial primary key,
			revision_id bigint not null references content_revisions(id) on delete restrict,
			path text not null,
			operation text not null,
			before_value jsonb,
			after_value jsonb,
			check(operation in ('add','remove','replace'))
		)`,
		`create index idx_content_change_items_revision on content_change_items(revision_id,id)`,
		`create table audit_events (
			id bigserial primary key,
			entity_id text references catalog_entities(id) on delete restrict,
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
		`create index idx_audit_events_entity on audit_events(entity_id,created_at desc,id desc) where entity_id is not null`,
		`create index idx_audit_events_aggregate on audit_events(aggregate_type,aggregate_key,created_at desc,id desc)`,
		`alter table knowledge_pages add constraint fk_knowledge_pages_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table tag_member_overrides add constraint fk_tag_member_overrides_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table recipe_type_catalyst_overrides add constraint fk_recipe_type_overrides_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table recipe_content_overrides add constraint fk_recipe_content_overrides_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table users add constraint fk_users_avatar_file foreign key(avatar_file_id) references oss_files(id) on delete set null`,
		`alter table mod_relationships add constraint fk_mod_relationships_group foreign key(group_id) references mod_relationship_groups(id) on delete cascade`,
		`alter table mods add constraint fk_mods_published_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table users add constraint fk_users_profile_revision foreign key(profile_revision_id) references content_revisions(id) on delete restrict`,
		`alter table blueprints add constraint fk_blueprints_published_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
	}
}

func immutableHistoryGuardStatements() []string {
	return []string{
		`create or replace function prevent_immutable_history_mutation() returns trigger as $$
		begin raise exception '% is append-only',tg_table_name; end;
		$$ language plpgsql`,
		`create trigger trg_content_revisions_immutable before update or delete on content_revisions for each row execute function prevent_immutable_history_mutation()`,
		`create trigger trg_review_events_immutable before update or delete on review_events for each row execute function prevent_immutable_history_mutation()`,
		`create trigger trg_content_change_items_immutable before update or delete on content_change_items for each row execute function prevent_immutable_history_mutation()`,
		`create trigger trg_audit_events_immutable before update or delete on audit_events for each row execute function prevent_immutable_history_mutation()`,
		`create trigger trg_permission_audit_logs_immutable before update or delete on permission_audit_logs for each row execute function prevent_immutable_history_mutation()`,
	}
}
