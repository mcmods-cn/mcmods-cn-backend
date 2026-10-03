package database

const searchServersForModFunctionSQL = `create or replace function enqueue_search_servers_for_mod() returns trigger as $$
		declare target_id bigint;
		begin
			target_id=case when TG_OP='DELETE' then old.id else new.id end;
			insert into search_index_queue(document_type,document_id,operation)
			select distinct 'server',server_id,'upsert' from minecraft_server_mods where mod_id=target_id
			on conflict(document_type,document_id) do update set
				operation='upsert',attempts=0,available_at=now(),last_error='',updated_at=now();
			if TG_OP='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql`

const searchServersForModParentFunctionSQL = `create or replace function enqueue_search_servers_for_mod_parent() returns trigger as $$
		declare old_id bigint; new_id bigint;
		begin
			if TG_OP<>'INSERT' then old_id=old.mod_id; end if;
			if TG_OP<>'DELETE' then new_id=new.mod_id; end if;
			insert into search_index_queue(document_type,document_id,operation)
			select distinct 'server',server_id,'upsert' from minecraft_server_mods
			where mod_id=old_id or mod_id=new_id
			on conflict(document_type,document_id) do update set
				operation='upsert',attempts=0,available_at=now(),last_error='',updated_at=now();
			if TG_OP='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql`

const changelogPopularityFunctionSQL = `create or replace function record_changelog_popularity_event() returns trigger as $$
		begin
			if not (old.review_status='approved' and old.status='active')
				and new.review_status='approved' and new.status='active' then
				perform record_popularity_event(new.object_route_id,'release',5);
				perform enqueue_content_stats_refresh(new.object_route_id,true,true);
			elsif old.review_status='approved' and old.status='active'
				and not (new.review_status='approved' and new.status='active') then
				perform record_popularity_event(new.object_route_id,'release',-5);
				perform enqueue_content_stats_refresh(new.object_route_id,true,true);
			end if;
			return new;
		end;
		$$ language plpgsql`
