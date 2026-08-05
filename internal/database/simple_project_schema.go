package database

// simpleProjectSchemaStatements installs the shared catalog used by plugins,
// maps, resource packs, shader packs, datapacks and add-on resources. These
// project families share publication, localization, authors, links, gallery,
// downloads and community references; project_type and the normalized option
// columns contain the intentional differences between them.
func simpleProjectSchemaStatements() []string {
	return []string{
		`create table simple_projects (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			project_type text not null check(project_type in ('plugin','map','resource_pack','shader_pack','datapack','addon')),
			slug text not null,
			default_locale text not null default 'zh-CN',
			primary_name text not null,
			summary text not null default '',
			body_markdown text not null default '',
			abbreviation text not null default '',
			minecraft_versions text[] not null default '{}'::text[],
			loaders text[] not null default '{}'::text[],
			categories text[] not null default '{}'::text[],
			features text[] not null default '{}'::text[],
			resolution text not null default '',
			performance text not null default '',
			map_size text not null default '',
			official_status text not null default 'development',
			source_status text not null default 'unknown',
			license text not null default 'Custom',
			curseforge_project_id text not null default '',
			modrinth_project_id text not null default '',
			icon_url text not null default '',
			search_keywords text[] not null default '{}'::text[],
			submission_method text not null default 'manual',
			review_status text not null default 'pending',
			created_by bigint references users(id) on delete set null,
			published_revision_id bigint references content_revisions(id) on delete restrict,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			published_at timestamptz,
			unique(project_type,slug),
			check(official_status in ('active','lowFrequency','discontinued','archived','development')),
			check(source_status in ('open','partial','closed','unknown')),
			check(submission_method in ('manual','modrinth','curseforge')),
			check(review_status in ('pending','approved','rejected'))
		)`,
		`create index idx_simple_projects_catalog on simple_projects(project_type,review_status,updated_at desc,id desc)`,
		`create index idx_simple_projects_created_by on simple_projects(created_by,updated_at desc)`,
		`create index idx_simple_projects_versions on simple_projects using gin(minecraft_versions)`,
		`create index idx_simple_projects_categories on simple_projects using gin(categories)`,
		`create table simple_project_localizations (
			project_id bigint not null references simple_projects(id) on delete cascade,
			locale text not null,
			name text not null,
			summary text not null default '',
			body_markdown text not null default '',
			primary key(project_id,locale)
		)`,
		`create index idx_simple_project_localizations_name on simple_project_localizations(lower(name))`,
		`create table simple_project_links (
			id bigserial primary key,
			project_id bigint not null references simple_projects(id) on delete cascade,
			link_type text not null,
			url text not null,
			note text not null default '',
			display_order integer not null default 0,
			unique(project_id,link_type,url)
		)`,
		`create index idx_simple_project_links_order on simple_project_links(project_id,display_order,id)`,
		`create table simple_project_gallery_images (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			project_id bigint not null references simple_projects(id) on delete cascade,
			oss_file_id bigint not null references oss_files(id) on delete restrict,
			display_order integer not null default 0,
			created_by bigint references users(id) on delete set null,
			published_revision_id bigint references content_revisions(id) on delete restrict,
			created_at timestamptz not null default now(),
			unique(project_id,oss_file_id)
		)`,
		`create index idx_simple_project_gallery_order on simple_project_gallery_images(project_id,display_order,id)`,
		`create table simple_project_parent_refs (
			id bigserial primary key,
			project_id bigint not null references simple_projects(id) on delete cascade,
			target_type text not null,
			target_id bigint,
			raw_identifier text not null default '',
			display_order integer not null default 0,
			foreign key(target_type,target_id) references public_routes(entity_type,internal_id) on delete restrict,
			check((target_id is null)<>(raw_identifier=''))
		)`,
		`create unique index idx_simple_project_parent_identity
			on simple_project_parent_refs(project_id,target_type,coalesce(target_id,0),raw_identifier)`,
		`create index idx_simple_project_parent_target
			on simple_project_parent_refs(target_type,target_id,project_id) where target_id is not null`,
		`create or replace function prevent_simple_project_identity_update() returns trigger as $$
		begin
			if new.public_id is distinct from old.public_id or new.project_type is distinct from old.project_type then
				raise exception 'simple project public ID and type are immutable';
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_simple_project_identity_immutable before update of public_id,project_type on simple_projects
			for each row execute function prevent_simple_project_identity_update()`,
		`create or replace function simple_project_path(project_type text,slug text) returns text as $$
		select case project_type
			when 'plugin' then '/plugins/'
			when 'map' then '/maps/'
			when 'resource_pack' then '/resource-packs/'
			when 'shader_pack' then '/shaders/'
			when 'datapack' then '/datapacks/'
			else '/addons/' end || slug
		$$ language sql immutable strict`,
		`create or replace function register_simple_project_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id,canonical_path)
			values(new.public_id,new.project_type,new.id,simple_project_path(new.project_type,new.slug));
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_simple_projects_public_route after insert on simple_projects
			for each row execute function register_simple_project_public_route()`,
		`create or replace function update_simple_project_public_route() returns trigger as $$
		begin
			update public_routes set canonical_path=simple_project_path(new.project_type,new.slug),updated_at=now()
			where public_id=new.public_id and entity_type=new.project_type;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_simple_projects_update_public_route after update of slug on simple_projects
			for each row execute function update_simple_project_public_route()`,
		`create or replace function remove_simple_project_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type=old.project_type;
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_simple_projects_remove_public_route after delete on simple_projects
			for each row execute function remove_simple_project_public_route()`,
		`create or replace function remove_simple_project_parent_unresolved_reference() returns trigger as $$
		begin
			delete from unresolved_references where source_type='simple_project_parent' and source_id=old.id;
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_simple_project_parent_remove_unresolved after delete on simple_project_parent_refs
			for each row execute function remove_simple_project_parent_unresolved_reference()`,
	}
}
