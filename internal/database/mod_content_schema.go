package database

// modContentSchemaStatements installs the human-authored, version-aware mod
// catalog layer. Global resources remain immutable identities; these tables
// hold project versions, version-scoped section trees, and complete independent detail
// documents for each version without duplicating the global identity graph.
func modContentSchemaStatements() []string {
	return []string{
		`create table mod_content_versions (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			mod_id bigint not null references mods(id) on delete cascade,
			label text not null default '',
			minecraft_versions text[] not null default '{}'::text[],
			loaders text[] not null default '{}'::text[],
			mod_version text not null default '',
			status text not null default 'active',
			published_revision_id bigint references content_revisions(id) on delete restrict,
			created_by bigint references users(id) on delete set null,
			updated_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check(status in ('active','pending','superseded','archived'))
		)`,
		`create index idx_mod_content_versions_mod_status on mod_content_versions(mod_id,status,updated_at desc)`,
		`alter table recipe_definitions add column source_mod_content_version_id bigint
			references mod_content_versions(id) on delete set null`,
		`create index idx_recipe_definitions_source_version on recipe_definitions(source_mod_content_version_id)
			where source_mod_content_version_id is not null`,
		`alter table catalog_import_jobs add constraint fk_catalog_import_jobs_target_version
			foreign key(target_version_id) references mod_content_versions(id) on delete cascade`,
		`alter table catalog_import_revisions add constraint fk_catalog_import_revisions_target_version
			foreign key(target_version_id) references mod_content_versions(id) on delete cascade`,
		`create table mod_content_templates (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			owner_mod_id bigint references mods(id) on delete cascade,
			code text not null,
			builtin boolean not null default false,
			i18n_key text not null default '',
			default_locale text not null default 'en-US',
			default_display_mode text not null default 'compact',
			definition jsonb not null default '{}'::jsonb,
			status text not null default 'active',
			published_revision_id bigint references content_revisions(id) on delete restrict,
			created_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check(code ~ '^[a-z][a-z0-9_]{1,63}$'),
			check(default_display_mode in ('compact','large')),
			check(status in ('active','pending','archived')),
			check(jsonb_typeof(definition)='object')
		)`,
		`create unique index idx_mod_content_templates_builtin_code on mod_content_templates(code) where builtin`,
		`create unique index idx_mod_content_templates_custom_code on mod_content_templates(owner_mod_id,code) where not builtin`,
		`create table mod_content_template_localizations (
			template_id bigint not null references mod_content_templates(id) on delete cascade,
			locale text not null,
			name text not null,
			description text not null default '',
			primary key(template_id,locale),
			check(locale ~ '^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$')
		)`,
		`insert into mod_content_templates(code,builtin,i18n_key,default_display_mode,definition) values
			('item_block',true,'itemBlock','compact','{"resourceKinds":["minecraft.item","minecraft.block"]}'::jsonb),
			('fluid',true,'fluid','compact','{"resourceKinds":["minecraft.fluid"]}'::jsonb),
			('dimension',true,'dimension','large','{"resourceKinds":["minecraft.dimension"]}'::jsonb),
			('biome',true,'biome','large','{"resourceKinds":["minecraft.biome"]}'::jsonb),
			('entity',true,'entity','large','{"resourceKinds":["minecraft.entity_type"]}'::jsonb),
			('enchantment',true,'enchantment','large','{"resourceKinds":["minecraft.enchantment"]}'::jsonb),
			('mob_effect',true,'mobEffect','large','{"resourceKinds":["minecraft.mob_effect"]}'::jsonb),
			('multiblock',true,'multiblock','large','{"resourceKinds":["minecraft.multiblock"]}'::jsonb),
			('natural_generation',true,'naturalGeneration','large','{"resourceKinds":["minecraft.natural_generation"]}'::jsonb),
			('world_structure',true,'worldStructure','large','{"resourceKinds":["minecraft.structure"]}'::jsonb),
			('key_mapping',true,'keyMapping','large','{"resourceKinds":["minecraft.key_mapping"]}'::jsonb),
			('command',true,'command','large','{"resourceKinds":["minecraft.command"]}'::jsonb),
			('advancement',true,'advancement','large','{"resourceKinds":["minecraft.advancement"]}'::jsonb),
			('skill',true,'skill','large','{"resourceKinds":["mod.skill"]}'::jsonb),
			('element',true,'element','large','{"resourceKinds":["mod.element"]}'::jsonb),
			('chemical',true,'chemical','compact','{"resourceKinds":["mekanism.gas","mekanism.infusion","mekanism.pigment","mekanism.slurry"]}'::jsonb)
			on conflict do nothing`,
		`create table mod_content_sections (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			mod_id bigint not null references mods(id) on delete cascade,
			version_id bigint not null references mod_content_versions(id) on delete cascade,
			template_id bigint not null references mod_content_templates(id) on delete restrict,
			parent_id bigint references mod_content_sections(id) on delete cascade,
			system_key text not null default '',
			default_locale text not null default 'en-US',
			display_mode text not null,
			ordinal integer not null default 0 check(ordinal>=0),
			status text not null default 'active',
			published_revision_id bigint references content_revisions(id) on delete restrict,
			created_by bigint references users(id) on delete set null,
			updated_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check(display_mode in ('compact','large')),
			check(status in ('active','pending','archived')),
			unique(id,version_id),
			unique(version_id,parent_id,ordinal)
		)`,
		`create index idx_mod_content_sections_tree on mod_content_sections(version_id,parent_id,ordinal)`,
		`create unique index idx_mod_content_sections_system_key on mod_content_sections(version_id,parent_id,system_key)
			where system_key<>'' and status='active'`,
		`create table mod_content_section_localizations (
			section_id bigint not null references mod_content_sections(id) on delete cascade,
			locale text not null,
			name text not null default '',
			description text not null default '',
			primary key(section_id,locale),
			check(locale ~ '^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$')
		)`,
		`create table mod_content_section_resources (
			section_id bigint not null,
			version_id bigint not null references mod_content_versions(id) on delete cascade,
			resource_id bigint not null references game_resources(entity_id) on delete restrict,
			placement_identity_key text not null,
			ordinal integer not null default 0 check(ordinal>=0),
			placement_source text not null default 'manual',
			created_at timestamptz not null default now(),
			primary key(section_id,version_id,resource_id),
			foreign key(section_id,version_id) references mod_content_sections(id,version_id) on delete cascade,
			unique(section_id,version_id,ordinal),
			unique(version_id,resource_id),
			unique(version_id,placement_identity_key),
			check(placement_source in ('manual','import'))
		)`,
		`create index idx_mod_content_section_resources_resource on mod_content_section_resources(resource_id,version_id)`,
		`create or replace function assign_mod_content_placement_identity() returns trigger as $$
		declare resource_kind text; resource_canonical_id text; block_representative_id bigint;
		begin
			select kind_code,canonical_id into strict resource_kind,resource_canonical_id
			from game_resources where entity_id=new.resource_id;
			if resource_kind in ('minecraft.item','minecraft.block') then
				select binding.block_resource_id into block_representative_id
				from game_resource_asset_bindings binding
				join resource_import_snapshots snapshot on snapshot.id=binding.snapshot_id
				join catalog_import_revisions revision on revision.id=snapshot.revision_id
				where revision.target_version_id=new.version_id and revision.is_active
				  and revision.status in ('ready','partial')
				  and binding.block_resource_id is not null
				  and new.resource_id in (binding.item_resource_id,binding.block_resource_id)
				order by coalesce(revision.activated_at,revision.created_at) desc,binding.block_resource_id
				limit 1;
				if block_representative_id is not null then
					new.placement_identity_key := 'item-block:resource:'||block_representative_id::text;
				else
					new.placement_identity_key := 'item-block:canonical:'||lower(resource_canonical_id);
				end if;
			else
				new.placement_identity_key := 'resource:'||new.resource_id::text;
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_mod_content_placement_identity before insert or update of resource_id,version_id
			on mod_content_section_resources for each row execute function assign_mod_content_placement_identity()`,
		`create table mod_resource_bindings (
			resource_id bigint primary key references game_resources(entity_id) on delete cascade,
			mod_id bigint not null references mods(id) on delete cascade,
			created_at timestamptz not null default now()
		)`,
		`create index idx_mod_resource_bindings_mod on mod_resource_bindings(mod_id,resource_id)`,
		`create table mod_resource_version_details (
			resource_id bigint not null references mod_resource_bindings(resource_id) on delete cascade,
			version_id bigint not null references mod_content_versions(id) on delete cascade,
			default_locale text not null default 'en-US',
			definition jsonb not null default '{}'::jsonb,
			icon_file_id bigint references oss_files(id) on delete set null,
			render_file_id bigint references oss_files(id) on delete set null,
			status text not null default 'active',
			published_revision_id bigint references content_revisions(id) on delete restrict,
			created_by bigint references users(id) on delete set null,
			updated_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			primary key(resource_id,version_id),
			check(status in ('active','pending','archived')),
			check(jsonb_typeof(definition)='object')
		)`,
		`create index idx_mod_resource_version_details_status on mod_resource_version_details(version_id,status,updated_at desc)`,
		`create table mod_resource_version_detail_localizations (
			resource_id bigint not null,
			version_id bigint not null,
			locale text not null,
			name text not null default '',
			summary text not null default '',
			content_markdown text not null default '',
			provenance text not null default 'human',
			primary key(resource_id,version_id,locale),
			foreign key(resource_id,version_id) references mod_resource_version_details(resource_id,version_id) on delete cascade,
			check(locale ~ '^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$'),
			check(provenance in ('human','import','ai','human_corrected'))
		)`,
		`create or replace function validate_mod_content_section_tree() returns trigger as $$
		declare parent_mod bigint; parent_version bigint; parent_depth integer;
		begin
			if new.parent_id is null then return new; end if;
			with recursive parents as (
				select section.id,section.parent_id,section.mod_id,section.version_id,1 depth from mod_content_sections section where section.id=new.parent_id
				union all select section.id,section.parent_id,section.mod_id,section.version_id,parents.depth+1
				from mod_content_sections section join parents on section.id=parents.parent_id where parents.depth<6
			) select max(mod_id),max(version_id),max(depth) into parent_mod,parent_version,parent_depth from parents;
			if parent_mod is null or parent_mod<>new.mod_id then raise exception 'section parent must belong to the same mod'; end if;
			if parent_version<>new.version_id then raise exception 'section parent must belong to the same data version'; end if;
			if parent_depth>=5 then raise exception 'content category depth cannot exceed four levels'; end if;
			if new.parent_id=new.id then raise exception 'content section cannot be its own parent'; end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_mod_content_section_tree before insert or update of parent_id,mod_id,version_id on mod_content_sections
			for each row execute function validate_mod_content_section_tree()`,
		`create or replace function register_mod_content_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id)
			values(new.public_id,TG_ARGV[0],new.id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_mod_content_versions_public_route after insert on mod_content_versions
			for each row execute function register_mod_content_public_route('mod_content_version')`,
		`create trigger trg_mod_content_templates_public_route after insert on mod_content_templates
			for each row execute function register_mod_content_public_route('mod_content_template')`,
		`create trigger trg_mod_content_sections_public_route after insert on mod_content_sections
			for each row execute function register_mod_content_public_route('mod_content_section')`,
		`insert into public_routes(public_id,entity_type,internal_id)
			select public_id,'mod_content_version',id from mod_content_versions on conflict do nothing`,
		`insert into public_routes(public_id,entity_type,internal_id)
			select public_id,'mod_content_template',id from mod_content_templates on conflict do nothing`,
		`insert into public_routes(public_id,entity_type,internal_id)
			select public_id,'mod_content_section',id from mod_content_sections on conflict do nothing`,
	}
}
