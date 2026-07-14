package database

// catalogSchemaStatements defines the durable identity layer used by every
// imported Minecraft object. Import revisions are snapshots; catalog entities
// are stable identities shared by revisions, mods and user-authored content.
func catalogSchemaStatements() []string {
	return []string{
		`create table mod_export_capabilities (
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			capability_id text not null,
			status text not null,
			source text not null default '',
			data jsonb not null default '{}'::jsonb,
			primary key(revision_id,capability_id)
		)`,
		`create table mod_export_revision_stats (
			revision_id text primary key references mod_export_revisions(id) on delete cascade,
			registry_counts jsonb not null default '{}'::jsonb,
			document_counts jsonb not null default '{}'::jsonb,
			capability_statuses jsonb not null default '{}'::jsonb,
			asset_count integer not null default 0,
			structure_count integer not null default 0,
			advancement_count integer not null default 0,
			key_mapping_count integer not null default 0,
			recipe_count integer not null default 0,
			tag_count integer not null default 0,
			updated_at timestamptz not null default now()
		)`,
		`create table catalog_entities (
			id text primary key,
			public_id text not null unique,
			entity_type text not null,
			status text not null default 'active',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check (entity_type in ('resource','recipe','recipe_type','tag','structure','document')),
			check (status in ('active','placeholder','archived'))
		)`,
		`create index idx_catalog_entities_type_status on catalog_entities(entity_type,status,id)`,
		`create table resource_kinds (
			code text primary key,
			family text not null,
			display_order integer not null default 0,
			user_visible boolean not null default true,
			created_at timestamptz not null default now()
		)`,
		`insert into resource_kinds(code,family,display_order,user_visible) values
			('minecraft.item','item',10,true),
			('minecraft.block','block',20,true),
			('minecraft.fluid','fluid',30,true),
			('minecraft.entity_type','entity',40,true),
			('minecraft.mob_effect','effect',50,true),
			('minecraft.enchantment','enchantment',60,true),
			('minecraft.biome','worldgen',70,true),
			('minecraft.dimension','worldgen',80,true),
			('minecraft.key_mapping','key_mapping',90,true),
			('minecraft.advancement','advancement',100,true),
			('minecraft.loot_table','loot_table',110,true),
			('minecraft.structure','structure',120,true),
			('mekanism.gas','chemical',130,true),
			('mekanism.infusion','chemical',140,true),
			('mekanism.pigment','chemical',150,true),
			('mekanism.slurry','chemical',160,true),
			('jei.ingredient','ingredient',900,true),
			('import.document','document',1000,true)`,
		`create table game_resources (
			entity_id text primary key references catalog_entities(id) on delete cascade,
			kind_code text not null references resource_kinds(code),
			canonical_id text not null,
			namespace text not null,
			resource_path text not null,
			owner_mod_id bigint references mods(id) on delete set null,
			created_from_revision_id text references mod_export_revisions(id) on delete set null,
			resolved boolean not null default false,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			unique(kind_code,canonical_id)
		)`,
		`create index idx_game_resources_canonical on game_resources(canonical_id,kind_code)`,
		`create index idx_game_resources_namespace on game_resources(namespace,kind_code,resource_path)`,
		`create table game_resource_aliases (
			kind_code text not null references resource_kinds(code),
			alias_id text not null,
			resource_id text not null references game_resources(entity_id) on delete cascade,
			created_at timestamptz not null default now(),
			primary key(kind_code,alias_id)
		)`,
		`create table game_resource_snapshots (
			id text primary key,
			resource_id text not null references game_resources(entity_id) on delete cascade,
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			registry text not null,
			translation_key text not null default '',
			names jsonb not null default '{}'::jsonb,
			data jsonb not null default '{}'::jsonb,
			icon_path text not null default '',
			preview_path text not null default '',
			created_at timestamptz not null default now(),
			unique(resource_id,revision_id),
			unique(revision_id,registry,resource_id)
		)`,
		`create index idx_game_resource_snapshots_revision_registry on game_resource_snapshots(revision_id,registry,resource_id)`,
		`create index idx_game_resource_snapshots_resource on game_resource_snapshots(resource_id,revision_id)`,
		`create table game_resource_asset_bindings (
			snapshot_id text primary key references game_resource_snapshots(id) on delete cascade,
			item_resource_id text references game_resources(entity_id) on delete set null,
			block_resource_id text references game_resources(entity_id) on delete set null,
			blockstate_path text not null default '',
			item_model_path text not null default '',
			model_paths text[] not null default '{}'::text[],
			texture_paths text[] not null default '{}'::text[]
		)`,
		`create index idx_game_resource_asset_bindings_item on game_resource_asset_bindings(item_resource_id) where item_resource_id is not null`,
		`create table catalog_tags (
			entity_id text primary key references catalog_entities(id) on delete cascade,
			registry text not null,
			canonical_id text not null,
			unique(registry,canonical_id)
		)`,
		`create table catalog_tag_snapshots (
			id text primary key,
			tag_id text not null references catalog_tags(entity_id) on delete cascade,
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			member_count integer not null default 0,
			unique(tag_id,revision_id)
		)`,
		`create table catalog_tag_members (
			tag_snapshot_id text not null references catalog_tag_snapshots(id) on delete cascade,
			resource_id text references game_resources(entity_id) on delete set null,
			raw_member_id text not null,
			ordinal integer not null,
			primary key(tag_snapshot_id,raw_member_id)
		)`,
		`create index idx_catalog_tag_members_resource on catalog_tag_members(resource_id) where resource_id is not null`,
		`create table recipe_types (
			entity_id text primary key references catalog_entities(id) on delete cascade,
			canonical_id text not null unique
		)`,
		`create table recipe_type_snapshots (
			id text primary key,
			recipe_type_id text not null references recipe_types(entity_id) on delete cascade,
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			title_translation_key text not null default '',
			title_names jsonb not null default '{}'::jsonb,
			width integer not null default 0,
			height integer not null default 0,
			background_path text not null default '',
			background_contains_ingredients boolean not null default false,
			image_scale integer not null default 1,
			canvas jsonb not null default '{}'::jsonb,
			catalysts jsonb not null default '[]'::jsonb,
			recipe_count integer not null default 0,
			unique(recipe_type_id,revision_id)
		)`,
		`create table recipes (
			entity_id text primary key references catalog_entities(id) on delete cascade,
			recipe_type_id text not null references recipe_types(entity_id) on delete cascade,
			canonical_source_id text,
			semantic_fingerprint text not null,
			owner_mod_id bigint references mods(id) on delete set null,
			identity_source text not null,
			created_at timestamptz not null default now(),
			check(identity_source in ('minecraft_recipe','jei_category','generated_index'))
		)`,
		`create unique index idx_recipes_authoritative_identity on recipes(recipe_type_id,canonical_source_id) where canonical_source_id is not null`,
		`create index idx_recipes_semantic_identity on recipes(recipe_type_id,semantic_fingerprint)`,
		`create table recipe_snapshots (
			id text primary key,
			recipe_id text not null references recipes(entity_id) on delete cascade,
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			source_recipe_id text not null,
			source_id_kind text not null,
			layout_path text not null,
			background_path text not null default '',
			layout jsonb not null,
			compact_layout jsonb not null,
			unique(recipe_id,revision_id,layout_path)
		)`,
		`create index idx_recipe_snapshots_revision on recipe_snapshots(revision_id,recipe_id)`,
		`create table recipe_ingredients (
			id text primary key,
			recipe_snapshot_id text not null references recipe_snapshots(id) on delete cascade,
			role text not null,
			slot_index integer not null,
			alternative_index integer not null,
			resource_id text references game_resources(entity_id) on delete set null,
			tag_id text references catalog_tags(entity_id) on delete set null,
			raw_resource_id text not null default '',
			amount double precision not null default 1,
			ingredient_kind text not null,
			ingredient_type text not null,
			nbt_snbt text not null default '',
			check(role in ('input','output','catalyst')),
			unique(recipe_snapshot_id,role,slot_index,alternative_index,raw_resource_id)
		)`,
		`create index idx_recipe_ingredients_resource on recipe_ingredients(resource_id,role,recipe_snapshot_id) where resource_id is not null`,
		`create index idx_recipe_ingredients_tag on recipe_ingredients(tag_id,recipe_snapshot_id) where tag_id is not null`,
		`create table unresolved_resource_references (
			id text primary key,
			source_entity_id text not null references catalog_entities(id) on delete cascade,
			source_revision_id text references mod_export_revisions(id) on delete cascade,
			field_path text not null,
			kind_code text not null,
			raw_resource_id text not null,
			resolved_resource_id text references game_resources(entity_id) on delete set null,
			status text not null default 'pending',
			created_at timestamptz not null default now(),
			resolved_at timestamptz,
			unique(source_entity_id,source_revision_id,field_path,kind_code,raw_resource_id),
			check(status in ('pending','resolved','ignored'))
		)`,
		`create index idx_unresolved_resource_references_pending on unresolved_resource_references(kind_code,raw_resource_id) where status='pending'`,
		`create table knowledge_pages (
			entity_id text not null references catalog_entities(id) on delete cascade,
			locale text not null,
			content_markdown text not null default '',
			updated_by bigint references users(id) on delete set null,
			published_revision_id bigint,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			primary key(entity_id,locale)
		)`,
		`create table tag_member_overrides (
			tag_id text primary key references catalog_tags(entity_id) on delete cascade,
			resource_ids text[] not null default '{}'::text[],
			updated_by bigint references users(id) on delete set null,
			published_revision_id bigint,
			updated_at timestamptz not null default now()
		)`,
		`create table recipe_type_catalyst_overrides (
			recipe_type_id text primary key references recipe_types(entity_id) on delete cascade,
			catalyst_resource_ids text[] not null default '{}'::text[],
			updated_by bigint references users(id) on delete set null,
			published_revision_id bigint,
			updated_at timestamptz not null default now()
		)`,
		`create table recipe_content_overrides (
			recipe_id text primary key references recipes(entity_id) on delete cascade,
			note text not null default '',
			layout_override jsonb,
			updated_by bigint references users(id) on delete set null,
			published_revision_id bigint,
			updated_at timestamptz not null default now()
		)`,
	}
}
