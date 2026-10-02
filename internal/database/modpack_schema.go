package database

import (
	"fmt"
	"strings"

	"mcmods-cn-backend/internal/catalogpolicy"
)

// modpackSchemaStatements installs the modpack catalog. Modpacks share public
// project routes, creator bindings, reviewed revisions, comments and project
// downloads with other top-level resources while keeping pack-specific mod
// membership and compatibility data normalized here.
func modpackSchemaStatements() []string {
	return []string{
		`alter table mod_metadata_import_jobs add column project_type text not null default 'mod'
			check(project_type in ('mod','modpack','plugin','map','resource_pack','shader_pack','datapack','addon'))`,
		`create index idx_mod_metadata_import_jobs_project_type on mod_metadata_import_jobs(project_type,user_id,created_at desc)`,
		fmt.Sprintf(`create table modpacks (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			slug text not null unique,
			primary_name text not null,
			secondary_name text not null default '',
			abbreviation text not null default '',
			summary text not null default '',
			default_locale text not null default 'zh-CN',
			environment text not null default 'bothRequired',
			primary_category text not null default 'adventure',
			constraint modpacks_primary_category_check check(primary_category in (%s)),
			pack_type text not null default 'native',
			packaging_method text not null default 'other',
			official_status text not null default 'development',
			source_status text not null default 'unknown',
			license text not null default 'Custom',
			curseforge_project_id text not null default '',
			modrinth_project_id text not null default '',
			icon_url text not null default '',
			body_markdown text not null default '',
			search_keywords text[] not null default '{}'::text[],
			submission_method text not null default 'manual',
			review_status text not null default 'pending',
			submitted_by bigint references users(id) on delete set null,
			published_revision_id bigint references content_revisions(id) on delete restrict,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			published_at timestamptz,
			check(environment in ('clientOnly','serverOnly','bothRequired')),
			check(pack_type in ('native','customized')),
			check(packaging_method in ('curseforge','ftb','other_launcher','manual','atlauncher','modrinth','mcbbs','other')),
			check(official_status in ('active','lowFrequency','discontinued','archived','development')),
			check(source_status in ('open','partial','closed','unknown')),
			check(submission_method in ('manual','modrinth','curseforge')),
			check(review_status in ('pending','approved','rejected'))
		)`, quotedModpackCategories()),
		`create index idx_modpacks_catalog on modpacks(review_status,updated_at desc,id desc)`,
		`create index idx_modpacks_submitted_by on modpacks(submitted_by,updated_at desc)`,
		`create index idx_modpacks_primary_name_lower on modpacks(lower(primary_name))`,
		`create or replace function prevent_modpack_public_id_update() returns trigger as $$
		begin
			if new.public_id is distinct from old.public_id then
				raise exception 'modpack public ID is immutable';
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_modpack_public_id_immutable before update of public_id on modpacks
			for each row execute function prevent_modpack_public_id_update()`,
		`create or replace function register_modpack_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id,canonical_path)
			values(new.public_id,'modpack',new.id,'/modpacks/'||new.slug);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_modpacks_public_route after insert on modpacks
			for each row execute function register_modpack_public_route()`,
		`create or replace function update_modpack_public_route() returns trigger as $$
		begin
			update public_routes set canonical_path='/modpacks/'||new.slug,updated_at=now()
			where public_id=new.public_id and entity_type='modpack';
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_modpacks_update_public_route after update of slug on modpacks
			for each row execute function update_modpack_public_route()`,
		`create or replace function remove_modpack_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type='modpack';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_modpacks_remove_public_route after delete on modpacks
			for each row execute function remove_modpack_public_route()`,
		`create table modpack_loader_compatibilities (
			modpack_id bigint not null references modpacks(id) on delete cascade,
			loader text not null,
			minecraft_version text not null,
			primary key(modpack_id,loader,minecraft_version)
		)`,
		`create index idx_modpack_compatibilities_version on modpack_loader_compatibilities(minecraft_version,loader,modpack_id)`,
		`create table modpack_tags (
			modpack_id bigint not null references modpacks(id) on delete cascade,
			tag text not null,
			primary key(modpack_id,tag)
		)`,
		`create table modpack_links (
			id bigserial primary key,
			modpack_id bigint not null references modpacks(id) on delete cascade,
			link_type text not null,
			url text not null,
			note text not null default '',
			display_order integer not null default 0,
			unique(modpack_id,link_type,url)
		)`,
		`create index idx_modpack_links_order on modpack_links(modpack_id,display_order,id)`,
		`create table modpack_gallery_images (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			modpack_id bigint not null references modpacks(id) on delete cascade,
			oss_file_id bigint not null references oss_files(id) on delete restrict,
			display_order integer not null default 0,
			created_by bigint references users(id) on delete set null,
			published_revision_id bigint references content_revisions(id) on delete restrict,
			created_at timestamptz not null default now(),
			unique(modpack_id,oss_file_id)
		)`,
		`create index idx_modpack_gallery_order on modpack_gallery_images(modpack_id,display_order,id)`,
		`create table modpack_mods (
			id bigserial primary key,
			modpack_id bigint not null references modpacks(id) on delete cascade,
			mod_id bigint references mods(id) on delete restrict,
			provider text not null default 'manual',
			provider_project_id text not null default '',
			provider_version_id text not null default '',
			identifier text not null default '',
			mod_name text not null default '',
			file_name text not null default '',
			client_required boolean not null default true,
			server_required boolean not null default true,
			display_order integer not null default 0,
			created_at timestamptz not null default now(),
			check(provider in ('manual','modrinth','curseforge','index')),
			check(mod_id is not null or provider_project_id<>'' or identifier<>'')
		)`,
		`create unique index idx_modpack_mods_identity on modpack_mods(
			modpack_id,provider,provider_project_id,provider_version_id,file_name,identifier,coalesce(mod_id,0))`,
		`create index idx_modpack_mods_order on modpack_mods(modpack_id,display_order,id)`,
		`create index idx_modpack_mods_resolved on modpack_mods(mod_id,modpack_id) where mod_id is not null`,
		`create or replace function remove_modpack_mod_unresolved_reference() returns trigger as $$
		begin
			delete from unresolved_references where source_type='modpack_mod' and source_id=old.id;
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_modpack_mods_remove_unresolved after delete on modpack_mods
			for each row execute function remove_modpack_mod_unresolved_reference()`,
	}
}

func quotedModpackCategories() string {
	categories := catalogpolicy.ModpackCategories()
	for index, category := range categories {
		categories[index] = "'" + strings.ReplaceAll(category, "'", "''") + "'"
	}
	return strings.Join(categories, ",")
}
