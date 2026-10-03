-- Original generation-168 function definitions from baseline 95694cc.
-- Owned integration-test fixture only; do not run against application databases.

create or replace function enqueue_search_servers_for_mod() returns trigger as $$
		declare target_id bigint;
		begin
			target_id=case when TG_OP='DELETE' then old.id else new.id end;
			insert into search_index_queue(document_type,document_id,operation)
			select 'server',server_id,'upsert' from minecraft_server_mods where mod_id=target_id
			on conflict(document_type,document_id) do update set
				operation='upsert',attempts=0,available_at=now(),last_error='',updated_at=now();
			if TG_OP='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql;

create or replace function enqueue_search_servers_for_mod_parent() returns trigger as $$
		declare old_id bigint; new_id bigint;
		begin
			if TG_OP<>'INSERT' then old_id=old.mod_id; end if;
			if TG_OP<>'DELETE' then new_id=new.mod_id; end if;
			insert into search_index_queue(document_type,document_id,operation)
			select 'server',server_id,'upsert' from minecraft_server_mods
			where mod_id=old_id or mod_id=new_id
			on conflict(document_type,document_id) do update set
				operation='upsert',attempts=0,available_at=now(),last_error='',updated_at=now();
			if TG_OP='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql;

create or replace function record_changelog_popularity_event() returns trigger as $$
		begin
			if old.review_status<>'approved' and new.review_status='approved' and new.status='active' then
				perform record_popularity_event(new.object_route_id,'release',5);
				perform enqueue_content_stats_refresh(new.object_route_id,true,true);
			elsif old.review_status='approved' and (new.review_status<>'approved' or new.status<>'active') then
				perform record_popularity_event(new.object_route_id,'release',-5);
				perform enqueue_content_stats_refresh(new.object_route_id,true,true);
			end if;
			return new;
		end;
		$$ language plpgsql;

create or replace function refresh_popularity_from_comment() returns trigger as $$
	declare
		route_id bigint; comment_author_id bigint; trust numeric;
		old_published boolean; new_published boolean; old_metric boolean:=false; new_metric boolean:=false; author_count bigint;
	begin
		route_id:=content_route_for_comment(
			case when tg_op='DELETE' then old.target_type else new.target_type end,
			case when tg_op='DELETE' then old.target_id else new.target_id end,
			case when tg_op='DELETE' then old.target_version_id else new.target_version_id end);
		if route_id is null then if tg_op='DELETE' then return old; end if; return new; end if;
		comment_author_id:=case when tg_op='DELETE' then old.author_id else new.author_id end;
		trust:=coalesce(content_user_trust(comment_author_id),0.2);
		old_published:=tg_op<>'INSERT' and old.status='published'
			and not content_target_user_is_developer(route_id,old.author_id);
		new_published:=tg_op<>'DELETE' and new.status='published'
			and not content_target_user_is_developer(route_id,new.author_id);
		old_metric:=tg_op<>'INSERT' and old.status='published' and content_popularity_actor_eligible(route_id,old.author_id);
		new_metric:=tg_op<>'DELETE' and new.status='published' and content_popularity_actor_eligible(route_id,new.author_id);
		select count(*) into author_count from comments comment
		where comment.status='published' and comment.author_id=comment_author_id
		  and content_route_for_comment(comment.target_type,comment.target_id,comment.target_version_id)=route_id;
		perform adjust_content_popularity_lifetime_facts(route_id,0,0,0,0,
			case when new_metric then 1 else 0 end-case when old_metric then 1 else 0 end,
			case when not old_metric and new_metric and author_count=1 then 1
				when old_metric and not new_metric and author_count=0 then -1 else 0 end,0,0);
		if not old_published and new_published and author_count=1 then perform record_popularity_event(route_id,'comment',3*trust);
		elsif old_published and not new_published and author_count=0 then perform record_popularity_event(route_id,'comment',-3*trust); end if;
		perform enqueue_content_stats_refresh(route_id,true,true);
		if tg_op='DELETE' then return old; end if; return new;
	end;
	$$ language plpgsql;

create trigger trg_comments_popularity after insert or update of status or delete on comments for each row execute function refresh_popularity_from_comment();
