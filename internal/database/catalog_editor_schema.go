package database

// catalogEditorSchemaStatements installs the canonical, human-editable catalog
// layer. Import tables are observations only; public reads and editor writes use
// these tables as the published source of truth.
func catalogEditorSchemaStatements() []string {
	return []string{
		`alter table catalog_entities drop constraint catalog_entities_entity_type_check`,
		`alter table catalog_entities add constraint catalog_entities_entity_type_check
			check (entity_type in ('resource','recipe','recipe_type','recipe_template','tag','structure','document'))`,
		`alter table catalog_entities
			add column default_locale text not null default 'en-US',
			add column published_revision_id bigint,
			add column archived_at timestamptz`,
		`alter table users add column secondary_content_language text not null default 'en-US'`,
		`create table content_subjects (
			subject_type text not null,
			subject_id bigint not null,
			default_locale text not null default 'en-US',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			primary key(subject_type,subject_id),
			foreign key(subject_type,subject_id) references public_routes(entity_type,internal_id) on delete cascade,
			check(default_locale ~ '^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$')
		)`,
		`insert into content_subjects(subject_type,subject_id)
			select entity_type,internal_id from public_routes on conflict do nothing`,
		`create or replace function register_content_subject() returns trigger as $$
		begin
			insert into content_subjects(subject_type,subject_id) values(new.entity_type,new.internal_id)
			on conflict(subject_type,subject_id) do nothing;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_public_routes_content_subject after insert on public_routes
			for each row execute function register_content_subject()`,
		`create or replace function sync_catalog_content_subject_locale() returns trigger as $$
		begin
			update content_subjects set default_locale=new.default_locale,updated_at=now()
			where subject_id=new.id and subject_type=new.entity_type;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_catalog_entities_content_subject_locale after update of default_locale on catalog_entities
			for each row execute function sync_catalog_content_subject_locale()`,
		`create table content_localizations (
			subject_type text not null,
			subject_id bigint not null,
			catalog_entity_id bigint references catalog_entities(id) on delete cascade,
			locale text not null,
			name text not null default '',
			summary text not null default '',
			content_markdown text not null default '',
			provenance text not null default 'human',
			source_locale text not null default '',
			ai_task_id bigint references ai_tasks(id) on delete set null,
			revision_no bigint not null default 1 check(revision_no>0),
			editable boolean not null default true,
			review_status text not null default 'approved',
			published_revision_id bigint,
			updated_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			primary key(subject_type,subject_id,locale),
			foreign key(subject_type,subject_id) references content_subjects(subject_type,subject_id) on delete cascade,
			foreign key(catalog_entity_id,subject_type) references catalog_entities(id,entity_type) on delete cascade,
			check(catalog_entity_id is null or catalog_entity_id=subject_id),
			check(locale ~ '^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$'),
			check(provenance in ('human','import','ai','human_corrected')),
			check(review_status in ('pending','approved','rejected'))
		)`,
		`create index idx_content_localizations_catalog on content_localizations(catalog_entity_id,locale) where catalog_entity_id is not null`,
		`create index idx_content_localizations_locale_name on content_localizations(locale,lower(name),subject_type,subject_id)`,
		`create table catalog_resource_definitions (
			resource_id bigint primary key references game_resources(entity_id) on delete cascade,
			definition_schema_version smallint not null default 1,
			definition jsonb not null default '{}'::jsonb,
			icon_file_id bigint references oss_files(id) on delete set null,
			render_file_id bigint references oss_files(id) on delete set null,
			published_revision_id bigint,
			updated_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check(jsonb_typeof(definition)='object'),
			check(definition_schema_version>=1)
		)`,
		`create table catalog_tag_members (
			tag_id bigint not null references catalog_tags(entity_id) on delete cascade,
			resource_id bigint not null references game_resources(entity_id) on delete restrict,
			ordinal integer not null check(ordinal>=0),
			published_revision_id bigint,
			primary key(tag_id,resource_id),
			unique(tag_id,ordinal)
		)`,
		`create index idx_catalog_tag_members_resource on catalog_tag_members(resource_id,tag_id)`,
		`create table recipe_type_definitions (
			recipe_type_id bigint primary key references recipe_types(entity_id) on delete cascade,
			definition jsonb not null default '{}'::jsonb,
			published_revision_id bigint,
			updated_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check(jsonb_typeof(definition)='object')
		)`,
		`create table recipe_type_catalysts (
			recipe_type_id bigint not null references recipe_types(entity_id) on delete cascade,
			resource_id bigint not null references game_resources(entity_id) on delete restrict,
			ordinal integer not null check(ordinal>=0),
			published_revision_id bigint,
			primary key(recipe_type_id,resource_id),
			unique(recipe_type_id,ordinal)
		)`,
		`create table recipe_layout_templates (
			entity_id bigint primary key references catalog_entities(id) on delete cascade,
			recipe_type_id bigint not null references recipe_types(entity_id) on delete restrict,
			template_key text not null,
			import_snapshot_id text references recipe_template_import_snapshots(id) on delete set null,
			background_file_id bigint references oss_files(id) on delete set null,
			canvas_width integer not null check(canvas_width>0 and canvas_width<=8192),
			canvas_height integer not null check(canvas_height>0 and canvas_height<=8192),
			image_scale integer not null default 1 check(image_scale between 1 and 32),
			definition jsonb not null default '{}'::jsonb,
			published_revision_id bigint,
			updated_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			unique(recipe_type_id,template_key),
			check(jsonb_typeof(definition)='object')
		)`,
		`create table recipe_template_slots (
			id bigserial primary key,
			identity_key text not null unique,
			template_id bigint not null references recipe_layout_templates(entity_id) on delete cascade,
			slot_key text not null,
			role text not null check(role in ('input','output','catalyst')),
			output_index integer,
			ordinal integer not null check(ordinal>=0),
			x numeric(12,4) not null,
			y numeric(12,4) not null,
			width numeric(12,4) not null check(width>0),
			height numeric(12,4) not null check(height>0),
			definition jsonb not null default '{}'::jsonb,
			unique(template_id,slot_key),
			unique(template_id,ordinal),
			check((role='output') or output_index is null),
			check(jsonb_typeof(definition)='object')
		)`,
		`alter table recipes drop constraint recipes_identity_source_check`,
		`alter table recipes add constraint recipes_identity_source_check
			check(identity_source in ('manual','import','minecraft_recipe','jei_category','generated_index'))`,
		`create table recipe_definitions (
			recipe_id bigint primary key references recipes(entity_id) on delete cascade,
			template_id bigint not null references recipe_layout_templates(entity_id) on delete restrict,
			definition_schema_version smallint not null default 1,
			definition jsonb not null default '{}'::jsonb,
			published_revision_id bigint,
			updated_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check(jsonb_typeof(definition)='object'),
			check(definition_schema_version>=1)
		)`,
		`create table recipe_bindings (
			id bigserial primary key,
			identity_key text not null unique,
			recipe_id bigint not null references recipes(entity_id) on delete cascade,
			template_slot_id bigint not null references recipe_template_slots(id) on delete restrict,
			ordinal integer not null check(ordinal>=0),
			definition jsonb not null default '{}'::jsonb,
			unique(recipe_id,template_slot_id),
			check(jsonb_typeof(definition)='object')
		)`,
		`create table recipe_binding_candidates (
			id bigserial primary key,
			identity_key text not null unique,
			binding_id bigint not null references recipe_bindings(id) on delete cascade,
			candidate_index integer not null check(candidate_index>=0),
			resource_id bigint not null references game_resources(entity_id) on delete restrict,
			amount numeric(20,6) not null default 1 check(amount>0),
			probability numeric(9,8),
			byproduct boolean not null default false,
			definition jsonb not null default '{}'::jsonb,
			unique(binding_id,candidate_index),
			check(probability is null or (probability>=0 and probability<=1)),
			check(jsonb_typeof(definition)='object')
		)`,
		`create index idx_recipe_binding_candidates_resource on recipe_binding_candidates(resource_id,binding_id)`,
		`create or replace function validate_recipe_candidate_output_fields() returns trigger as $$
		declare slot_role text;
		begin
			select slot.role into slot_role from recipe_bindings binding
			join recipe_template_slots slot on slot.id=binding.template_slot_id where binding.id=new.binding_id;
			if slot_role is null then raise exception 'recipe template slot does not exist'; end if;
			if slot_role <> 'output' and (new.probability is not null or new.byproduct) then
				raise exception 'probability and byproduct are only valid for output slots';
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_recipe_candidate_output_fields before insert or update on recipe_binding_candidates
			for each row execute function validate_recipe_candidate_output_fields()`,
		`create table catalog_dataset_state (
			singleton boolean primary key default true check(singleton),
			version bigint not null default 1 check(version>0),
			updated_at timestamptz not null default now()
		)`,
		`insert into catalog_dataset_state(singleton,version) values(true,1)`,
	}
}
