package database

// searchSchemaStatements installs the durable PostgreSQL side of the external
// search projection. PostgreSQL remains authoritative: transactions only
// enqueue a compact document identity and never wait for Typesense.
func searchSchemaStatements() []string {
	return []string{
		`create table search_index_state (
			collection_kind text primary key,
			schema_version integer not null check(schema_version>0),
			collection_name text not null,
			rebuilt_at timestamptz not null default now()
		)`,
		`create table search_index_rebuild_progress (
			collection_kind text primary key,
			collection_name text not null,
			document_type text not null default '',
			last_document_id bigint not null default 0 check(last_document_id>=0),
			indexed_document_count bigint not null default 0 check(indexed_document_count>=0),
			indexed_byte_count bigint not null default 0 check(indexed_byte_count>=0),
			batch_count bigint not null default 0 check(batch_count>=0),
			status text not null check(status in ('building','complete','failed')),
			last_error text not null default '',
			started_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`create table search_index_queue (
			document_type text not null check(document_type in ('mod','modpack','simple_project','creator','community_post','resource','server')),
			document_id bigint not null check(document_id>0),
			operation text not null default 'upsert' check(operation in ('upsert','delete')),
			attempts integer not null default 0 check(attempts>=0),
			available_at timestamptz not null default now(),
			last_error text not null default '',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			primary key(document_type,document_id)
		)`,
		`create index idx_search_index_queue_available on search_index_queue(available_at,updated_at)`,
		`create or replace function enqueue_search_index_document(input_type text,input_id bigint,input_operation text)
		returns void as $$
		begin
			if input_id is null or input_id<=0 then return; end if;
			insert into search_index_queue(document_type,document_id,operation)
			values(input_type,input_id,input_operation)
			on conflict(document_type,document_id) do update set
				operation=excluded.operation,attempts=0,available_at=now(),last_error='',updated_at=now();
		end;
		$$ language plpgsql`,
		`create or replace function enqueue_search_index_root() returns trigger as $$
		declare target_id bigint;
		begin
			if TG_OP='DELETE' then
				target_id=old.id;
				perform enqueue_search_index_document(TG_ARGV[0],target_id,'delete');
				return old;
			end if;
			target_id=new.id;
			perform enqueue_search_index_document(TG_ARGV[0],target_id,'upsert');
			return new;
		end;
		$$ language plpgsql`,
		`create or replace function enqueue_search_index_parent() returns trigger as $$
		declare old_id bigint; new_id bigint;
		begin
			if TG_OP<>'INSERT' then
				old_id=nullif(to_jsonb(old)->>TG_ARGV[1],'')::bigint;
				perform enqueue_search_index_document(TG_ARGV[0],old_id,'upsert');
			end if;
			if TG_OP<>'DELETE' then
				new_id=nullif(to_jsonb(new)->>TG_ARGV[1],'')::bigint;
				if new_id is distinct from old_id then
					perform enqueue_search_index_document(TG_ARGV[0],new_id,'upsert');
				end if;
			end if;
			if TG_OP='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql`,
		`create or replace function enqueue_search_creator_bindings_statement() returns trigger as $$
		begin
			if TG_OP='INSERT' then
				insert into search_index_queue(document_type,document_id,operation)
				select distinct case when binding.subject_type='mod' then 'mod'
					when binding.subject_type='modpack' then 'modpack' else 'simple_project' end,
					binding.subject_id,'upsert' from creator_binding_new binding
				where binding.subject_type in ('mod','modpack','plugin','map','resource_pack','shader_pack','datapack','addon')
				on conflict(document_type,document_id) do update set operation='upsert',attempts=0,
					available_at=now(),last_error='',updated_at=now();
			elsif TG_OP='DELETE' then
				insert into search_index_queue(document_type,document_id,operation)
				select distinct case when binding.subject_type='mod' then 'mod'
					when binding.subject_type='modpack' then 'modpack' else 'simple_project' end,
					binding.subject_id,'upsert' from creator_binding_old binding
				where binding.subject_type in ('mod','modpack','plugin','map','resource_pack','shader_pack','datapack','addon')
				on conflict(document_type,document_id) do update set operation='upsert',attempts=0,
					available_at=now(),last_error='',updated_at=now();
			else
				insert into search_index_queue(document_type,document_id,operation)
				select distinct case when binding.subject_type='mod' then 'mod'
					when binding.subject_type='modpack' then 'modpack' else 'simple_project' end,
					binding.subject_id,'upsert' from (
						select subject_type,subject_id from creator_binding_old
						union select subject_type,subject_id from creator_binding_new
					) binding
				where binding.subject_type in ('mod','modpack','plugin','map','resource_pack','shader_pack','datapack','addon')
				on conflict(document_type,document_id) do update set operation='upsert',attempts=0,
					available_at=now(),last_error='',updated_at=now();
			end if;
			return null;
		end;
		$$ language plpgsql`,
		`create or replace function enqueue_search_content_localization() returns trigger as $$
		declare source jsonb; target_type text; target_id bigint; catalog_id bigint; document_type text; pass integer;
		begin
			for pass in 1..2 loop
				if pass=1 and TG_OP='INSERT' then continue; end if;
				if pass=2 and TG_OP='DELETE' then continue; end if;
				source=case when pass=1 then to_jsonb(old) else to_jsonb(new) end;
				catalog_id=nullif(source->>'catalog_entity_id','')::bigint;
				if catalog_id is not null then perform enqueue_search_index_document('resource',catalog_id,'upsert'); end if;
				target_type=source->>'subject_type'; target_id=nullif(source->>'subject_id','')::bigint;
				document_type=case target_type when 'mod' then 'mod' when 'modpack' then 'modpack'
					when 'plugin' then 'simple_project' when 'map' then 'simple_project'
					when 'resource_pack' then 'simple_project' when 'shader_pack' then 'simple_project'
					when 'datapack' then 'simple_project' when 'addon' then 'simple_project'
					when 'creator' then 'creator' else '' end;
				if document_type<>'' then perform enqueue_search_index_document(document_type,target_id,'upsert'); end if;
			end loop;
			if TG_OP='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql`,
		`create or replace function enqueue_search_catalog_entity() returns trigger as $$
		begin
			if TG_OP='DELETE' then
				if old.entity_type='resource' then perform enqueue_search_index_document('resource',old.id,'delete'); end if;
				return old;
			end if;
			if new.entity_type='resource' then perform enqueue_search_index_document('resource',new.id,'upsert'); end if;
			return new;
		end;
		$$ language plpgsql`,
		searchServersForModFunctionSQL,
		searchServersForModParentFunctionSQL,
		`create or replace function enqueue_search_server_popularity() returns trigger as $$
		declare route record;
		begin
			select public_route.internal_id,public_route.entity_type into route
			from public_routes public_route where public_route.id=new.object_route_id;
			if route.entity_type='minecraft_server' then
				perform enqueue_search_index_document('server',route.internal_id,'upsert');
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_search_mods after insert or update or delete on mods for each row execute function enqueue_search_index_root('mod')`,
		`create trigger trg_search_modpacks after insert or update or delete on modpacks for each row execute function enqueue_search_index_root('modpack')`,
		`create trigger trg_search_simple_projects after insert or update or delete on simple_projects for each row execute function enqueue_search_index_root('simple_project')`,
		`create trigger trg_search_creators after insert or update or delete on creators for each row execute function enqueue_search_index_root('creator')`,
		`create trigger trg_search_community_posts after insert or update or delete on community_posts for each row execute function enqueue_search_index_root('community_post')`,
		`create trigger trg_search_servers after insert or update or delete on minecraft_servers for each row execute function enqueue_search_index_root('server')`,
		`create trigger trg_search_catalog_entities after insert or update or delete on catalog_entities for each row execute function enqueue_search_catalog_entity()`,
		`create trigger trg_search_mod_identifiers after insert or update or delete on mod_identifiers for each row execute function enqueue_search_index_parent('mod','mod_id')`,
		`create trigger trg_search_mod_identifier_servers after insert or update or delete on mod_identifiers for each row execute function enqueue_search_servers_for_mod_parent()`,
		`create trigger trg_search_mod_server_names after update of primary_name,secondary_name,project_code or delete on mods for each row execute function enqueue_search_servers_for_mod()`,
		`create trigger trg_search_mod_loaders after insert or update or delete on mod_loader_compatibilities for each row execute function enqueue_search_index_parent('mod','mod_id')`,
		`create trigger trg_search_mod_tags after insert or update or delete on mod_tags for each row execute function enqueue_search_index_parent('mod','mod_id')`,
		`create trigger trg_search_modpack_loaders after insert or update or delete on modpack_loader_compatibilities for each row execute function enqueue_search_index_parent('modpack','modpack_id')`,
		`create trigger trg_search_modpack_tags after insert or update or delete on modpack_tags for each row execute function enqueue_search_index_parent('modpack','modpack_id')`,
		`create trigger trg_search_creator_bindings_insert after insert on content_creator_bindings
			referencing new table as creator_binding_new for each statement execute function enqueue_search_creator_bindings_statement()`,
		`create trigger trg_search_creator_bindings_update after update on content_creator_bindings
			referencing old table as creator_binding_old new table as creator_binding_new
			for each statement execute function enqueue_search_creator_bindings_statement()`,
		`create trigger trg_search_creator_bindings_delete after delete on content_creator_bindings
			referencing old table as creator_binding_old for each statement execute function enqueue_search_creator_bindings_statement()`,
		`create trigger trg_search_simple_project_localizations after insert or update or delete on simple_project_localizations for each row execute function enqueue_search_index_parent('simple_project','project_id')`,
		`create trigger trg_search_community_project_refs after insert or update or delete on community_post_project_refs for each row execute function enqueue_search_index_parent('community_post','post_id')`,
		`create trigger trg_search_community_resource_refs after insert or update or delete on community_post_resource_refs for each row execute function enqueue_search_index_parent('community_post','post_id')`,
		`create trigger trg_search_community_translations after insert or update or delete on community_post_translations for each row execute function enqueue_search_index_parent('community_post','post_id')`,
		`create trigger trg_search_server_mods after insert or update or delete on minecraft_server_mods for each row execute function enqueue_search_index_parent('server','server_id')`,
		`create trigger trg_search_server_popularity after insert or update on content_popularity_stats
			for each row execute function enqueue_search_server_popularity()`,
		`create trigger trg_search_resource_snapshots after insert or update or delete on resource_import_snapshots for each row execute function enqueue_search_index_parent('resource','resource_id')`,
		`create trigger trg_search_game_resources after insert or update or delete on game_resources for each row execute function enqueue_search_index_parent('resource','entity_id')`,
		`create trigger trg_search_content_localizations after insert or update or delete on content_localizations for each row execute function enqueue_search_content_localization()`,
	}
}
