package database

func unresolvedReferenceCatalogSchemaStatements() []string {
	return []string{
		`create table unresolved_reference_catalog (
			origin smallint not null,
			source_row_id bigint not null,
			source_type text not null,
			source_id bigint not null,
			field_path text not null,
			reference_type text not null,
			raw_identifier text not null,
			status text not null,
			resolved_type text not null default '',
			resolved_id text not null default '',
			source_label text not null default '',
			source_public_id text not null default '',
			created_at timestamptz not null,
			resolved_at timestamptz,
			primary key(origin,source_row_id),
			check(origin in (0,1)),
			check(status in ('pending','resolved','ignored'))
		)`,
		`create index idx_unresolved_reference_catalog_page
			on unresolved_reference_catalog(created_at desc,origin desc,source_row_id desc)`,
		`create index idx_unresolved_reference_catalog_status_page
			on unresolved_reference_catalog(status,created_at desc,origin desc,source_row_id desc)`,
		`create index idx_unresolved_reference_catalog_type_page
			on unresolved_reference_catalog(reference_type,created_at desc,origin desc,source_row_id desc)`,
		`create index idx_unresolved_reference_catalog_status_type_page
			on unresolved_reference_catalog(status,reference_type,created_at desc,origin desc,source_row_id desc)`,
		`create index idx_unresolved_reference_catalog_raw_prefix
			on unresolved_reference_catalog(lower(raw_identifier) text_pattern_ops)`,
		`create index idx_unresolved_reference_catalog_label_prefix
			on unresolved_reference_catalog(lower(source_label) text_pattern_ops)`,
		`create index idx_unresolved_reference_catalog_source
			on unresolved_reference_catalog(source_type,source_id)`,
		`create or replace function refresh_unresolved_reference_catalog_general(reference_id bigint) returns void as $$
		begin
			insert into unresolved_reference_catalog(
				origin,source_row_id,source_type,source_id,field_path,reference_type,raw_identifier,status,
				resolved_type,resolved_id,source_label,source_public_id,created_at,resolved_at)
			select 0,unresolved.id,unresolved.source_type,unresolved.source_id,unresolved.field_path,
				unresolved.reference_type,unresolved.raw_identifier,unresolved.status,unresolved.resolved_type,
				coalesce(unresolved.resolved_id::text,''),
				coalesce(source_mod.primary_name,community_post.title,source_modpack.primary_name,
					source_server.name,source_simple_project.primary_name,source_catalog_entity.identity_key,''),
				coalesce(source_mod.project_code,community_post.public_id,source_modpack.public_id,
					source_server.public_id,source_simple_project.public_id,source_catalog_entity.public_id,''),
				unresolved.created_at,unresolved.resolved_at
			from unresolved_references unresolved
			left join mod_relationships relationship
			  on unresolved.source_type='mod_relationship' and relationship.id=unresolved.source_id
			left join mods source_mod on source_mod.id=relationship.mod_id
			left join community_post_project_refs community_project
			  on unresolved.source_type='community_post_project' and community_project.id=unresolved.source_id
			left join community_post_resource_refs community_resource
			  on unresolved.source_type='community_post_resource' and community_resource.id=unresolved.source_id
			left join community_posts community_post
			  on community_post.id=coalesce(community_project.post_id,community_resource.post_id)
			left join modpack_mods modpack_entry
			  on unresolved.source_type='modpack_mod' and modpack_entry.id=unresolved.source_id
			left join modpacks source_modpack on source_modpack.id=modpack_entry.modpack_id
			left join minecraft_server_mods server_mod
			  on unresolved.source_type='minecraft_server_mod' and server_mod.id=unresolved.source_id
			left join minecraft_servers source_server on source_server.id=server_mod.server_id
			left join simple_project_parent_refs simple_parent
			  on unresolved.source_type='simple_project_parent' and simple_parent.id=unresolved.source_id
			left join simple_projects source_simple_project on source_simple_project.id=simple_parent.project_id
			left join catalog_entities source_catalog_entity
			  on unresolved.source_type='mod_content_resource' and source_catalog_entity.id=unresolved.source_id
			where unresolved.id=reference_id
			on conflict(origin,source_row_id) do update set
				source_type=excluded.source_type,source_id=excluded.source_id,field_path=excluded.field_path,
				reference_type=excluded.reference_type,raw_identifier=excluded.raw_identifier,status=excluded.status,
				resolved_type=excluded.resolved_type,resolved_id=excluded.resolved_id,
				source_label=excluded.source_label,source_public_id=excluded.source_public_id,
				created_at=excluded.created_at,resolved_at=excluded.resolved_at;
		end;
		$$ language plpgsql`,
		`create or replace function refresh_unresolved_reference_catalog_resource(reference_id bigint) returns void as $$
		begin
			insert into unresolved_reference_catalog(
				origin,source_row_id,source_type,source_id,field_path,reference_type,raw_identifier,status,
				resolved_type,resolved_id,source_label,source_public_id,created_at,resolved_at)
			select 1,unresolved.id,'catalog_resource',unresolved.source_entity_id,unresolved.field_path,
				unresolved.kind_code,unresolved.raw_resource_id,unresolved.status,'resource',
				coalesce(unresolved.resolved_resource_id::text,''),
				coalesce(recipe.canonical_source_id,source_entity.identity_key),source_entity.public_id,
				unresolved.created_at,unresolved.resolved_at
			from unresolved_resource_references unresolved
			join catalog_entities source_entity on source_entity.id=unresolved.source_entity_id
			left join recipes recipe on recipe.entity_id=unresolved.source_entity_id
			where unresolved.id=reference_id
			on conflict(origin,source_row_id) do update set
				source_type=excluded.source_type,source_id=excluded.source_id,field_path=excluded.field_path,
				reference_type=excluded.reference_type,raw_identifier=excluded.raw_identifier,status=excluded.status,
				resolved_type=excluded.resolved_type,resolved_id=excluded.resolved_id,
				source_label=excluded.source_label,source_public_id=excluded.source_public_id,
				created_at=excluded.created_at,resolved_at=excluded.resolved_at;
		end;
		$$ language plpgsql`,
		`create or replace function sync_unresolved_reference_catalog_general() returns trigger as $$
		begin
			if TG_OP='DELETE' then
				delete from unresolved_reference_catalog where origin=0 and source_row_id=old.id;
				return old;
			end if;
			perform refresh_unresolved_reference_catalog_general(new.id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_unresolved_reference_catalog_general
			after insert or update or delete on unresolved_references
			for each row execute function sync_unresolved_reference_catalog_general()`,
		`create or replace function sync_unresolved_reference_catalog_resource() returns trigger as $$
		begin
			if TG_OP='DELETE' then
				delete from unresolved_reference_catalog where origin=1 and source_row_id=old.id;
				return old;
			end if;
			perform refresh_unresolved_reference_catalog_resource(new.id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_unresolved_reference_catalog_resource
			after insert or update or delete on unresolved_resource_references
			for each row execute function sync_unresolved_reference_catalog_resource()`,
		`create or replace function refresh_unresolved_reference_catalog_source_link() returns trigger as $$
		declare source_kind text;
		begin
			source_kind := case TG_TABLE_NAME
				when 'mod_relationships' then 'mod_relationship'
				when 'community_post_project_refs' then 'community_post_project'
				when 'community_post_resource_refs' then 'community_post_resource'
				when 'modpack_mods' then 'modpack_mod'
				when 'minecraft_server_mods' then 'minecraft_server_mod'
				when 'simple_project_parent_refs' then 'simple_project_parent'
				else '' end;
			perform refresh_unresolved_reference_catalog_general(unresolved.id)
			from unresolved_references unresolved
			where unresolved.source_type=source_kind and unresolved.source_id=new.id;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_unresolved_reference_catalog_mod_relationship_link
			after update of mod_id on mod_relationships
			for each row execute function refresh_unresolved_reference_catalog_source_link()`,
		`create trigger trg_unresolved_reference_catalog_community_project_link
			after update of post_id on community_post_project_refs
			for each row execute function refresh_unresolved_reference_catalog_source_link()`,
		`create trigger trg_unresolved_reference_catalog_community_resource_link
			after update of post_id on community_post_resource_refs
			for each row execute function refresh_unresolved_reference_catalog_source_link()`,
		`create trigger trg_unresolved_reference_catalog_modpack_link
			after update of modpack_id on modpack_mods
			for each row execute function refresh_unresolved_reference_catalog_source_link()`,
		`create trigger trg_unresolved_reference_catalog_server_mod_link
			after update of server_id on minecraft_server_mods
			for each row execute function refresh_unresolved_reference_catalog_source_link()`,
		`create trigger trg_unresolved_reference_catalog_simple_parent_link
			after update of project_id on simple_project_parent_refs
			for each row execute function refresh_unresolved_reference_catalog_source_link()`,
		`create or replace function refresh_unresolved_reference_catalog_source_label() returns trigger as $$
		begin
			if TG_TABLE_NAME='mods' then
				perform refresh_unresolved_reference_catalog_general(unresolved.id)
				from unresolved_references unresolved join mod_relationships relationship
				  on unresolved.source_type='mod_relationship' and unresolved.source_id=relationship.id
				where relationship.mod_id=new.id;
			elsif TG_TABLE_NAME='community_posts' then
				perform refresh_unresolved_reference_catalog_general(unresolved.id)
				from unresolved_references unresolved join community_post_project_refs reference
				  on unresolved.source_type='community_post_project' and unresolved.source_id=reference.id
				where reference.post_id=new.id;
				perform refresh_unresolved_reference_catalog_general(unresolved.id)
				from unresolved_references unresolved join community_post_resource_refs reference
				  on unresolved.source_type='community_post_resource' and unresolved.source_id=reference.id
				where reference.post_id=new.id;
			elsif TG_TABLE_NAME='modpacks' then
				perform refresh_unresolved_reference_catalog_general(unresolved.id)
				from unresolved_references unresolved join modpack_mods entry
				  on unresolved.source_type='modpack_mod' and unresolved.source_id=entry.id
				where entry.modpack_id=new.id;
			elsif TG_TABLE_NAME='minecraft_servers' then
				perform refresh_unresolved_reference_catalog_general(unresolved.id)
				from unresolved_references unresolved join minecraft_server_mods server_mod
				  on unresolved.source_type='minecraft_server_mod' and unresolved.source_id=server_mod.id
				where server_mod.server_id=new.id;
			elsif TG_TABLE_NAME='simple_projects' then
				perform refresh_unresolved_reference_catalog_general(unresolved.id)
				from unresolved_references unresolved join simple_project_parent_refs parent_reference
				  on unresolved.source_type='simple_project_parent' and unresolved.source_id=parent_reference.id
				where parent_reference.project_id=new.id;
			elsif TG_TABLE_NAME='catalog_entities' then
				perform refresh_unresolved_reference_catalog_resource(unresolved.id)
				from unresolved_resource_references unresolved where unresolved.source_entity_id=new.id;
				perform refresh_unresolved_reference_catalog_general(unresolved.id)
				from unresolved_references unresolved
				where unresolved.source_type='mod_content_resource' and unresolved.source_id=new.id;
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_unresolved_reference_catalog_mod_label
			after update of primary_name,project_code on mods
			for each row execute function refresh_unresolved_reference_catalog_source_label()`,
		`create trigger trg_unresolved_reference_catalog_community_label
			after update of title,public_id on community_posts
			for each row execute function refresh_unresolved_reference_catalog_source_label()`,
		`create trigger trg_unresolved_reference_catalog_modpack_label
			after update of primary_name,public_id on modpacks
			for each row execute function refresh_unresolved_reference_catalog_source_label()`,
		`create trigger trg_unresolved_reference_catalog_server_label
			after update of name,public_id on minecraft_servers
			for each row execute function refresh_unresolved_reference_catalog_source_label()`,
		`create trigger trg_unresolved_reference_catalog_simple_project_label
			after update of primary_name,public_id on simple_projects
			for each row execute function refresh_unresolved_reference_catalog_source_label()`,
		`create trigger trg_unresolved_reference_catalog_entity_label
			after update of identity_key,public_id on catalog_entities
			for each row execute function refresh_unresolved_reference_catalog_source_label()`,
		`create or replace function refresh_unresolved_reference_catalog_recipe_label() returns trigger as $$
		declare target_id bigint;
		begin
			target_id := case when TG_OP='DELETE' then old.entity_id else new.entity_id end;
			perform refresh_unresolved_reference_catalog_resource(unresolved.id)
			from unresolved_resource_references unresolved where unresolved.source_entity_id=target_id;
			return case when TG_OP='DELETE' then old else new end;
		end;
		$$ language plpgsql`,
		`create trigger trg_unresolved_reference_catalog_recipe_label
			after insert or update of canonical_source_id or delete on recipes
			for each row execute function refresh_unresolved_reference_catalog_recipe_label()`,
	}
}
