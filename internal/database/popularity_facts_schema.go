package database

func popularityFactProjectionStatements() []string {
	return []string{
		`create table content_popularity_lifetime_facts (
			object_route_id bigint primary key references public_routes(id) on delete cascade,
			unique_view_count bigint not null default 0 check(unique_view_count>=0),
			page_count bigint not null default 0 check(page_count>=0),
			download_count bigint not null default 0 check(download_count>=0),
			favorite_count bigint not null default 0 check(favorite_count>=0),
			comment_count bigint not null default 0 check(comment_count>=0),
			effective_commenter_count bigint not null default 0 check(effective_commenter_count>=0),
			rating_count bigint not null default 0 check(rating_count>=0),
			rating_sum bigint not null default 0 check(rating_sum>=0),
			calibrated_at timestamptz,
			updated_at timestamptz not null default now()
		)`,
		`create table content_popularity_view_totals (
			object_route_id bigint not null references public_routes(id) on delete cascade,
			counter_shard smallint not null check(counter_shard between 0 and 31),
			views bigint not null default 0 check(views>=0),
			updated_at timestamptz not null default now(),
			primary key(object_route_id,counter_shard)
		)`,
		`create table content_popularity_rating_dimension_facts (
			object_route_id bigint not null references public_routes(id) on delete cascade,
			dimension_code text not null,
			rating_count bigint not null default 0 check(rating_count>=0),
			score_sum bigint not null default 0 check(score_sum>=0),
			updated_at timestamptz not null default now(),
			primary key(object_route_id,dimension_code)
		)`,
		`create or replace function adjust_content_popularity_lifetime_facts(
			target_route_id bigint,unique_view_delta bigint,page_delta bigint,download_delta bigint,
			favorite_delta bigint,comment_delta bigint,commenter_delta bigint,rating_count_delta bigint,rating_sum_delta bigint
		) returns void as $$
		begin
			if target_route_id is null then return; end if;
			insert into content_popularity_lifetime_facts(object_route_id) values(target_route_id)
			on conflict(object_route_id) do nothing;
			update content_popularity_lifetime_facts set
				unique_view_count=unique_view_count+unique_view_delta,
				page_count=page_count+page_delta,
				download_count=download_count+download_delta,
				favorite_count=favorite_count+favorite_delta,
				comment_count=comment_count+comment_delta,
				effective_commenter_count=effective_commenter_count+commenter_delta,
				rating_count=rating_count+rating_count_delta,
				rating_sum=rating_sum+rating_sum_delta,
				updated_at=now()
			where object_route_id=target_route_id;
		end;
		$$ language plpgsql`,
		`create or replace function adjust_content_popularity_view_total(
			target_route_id bigint,target_shard smallint,view_delta bigint
		) returns void as $$
		begin
			if target_route_id is null or view_delta=0 then return; end if;
			insert into content_popularity_view_totals(object_route_id,counter_shard)
			values(target_route_id,target_shard) on conflict(object_route_id,counter_shard) do nothing;
			update content_popularity_view_totals set views=views+view_delta,updated_at=now()
			where object_route_id=target_route_id and counter_shard=target_shard;
			delete from content_popularity_view_totals
			where object_route_id=target_route_id and counter_shard=target_shard and views=0;
		end;
		$$ language plpgsql`,
		`create or replace function adjust_content_popularity_dimension_fact(
			target_route_id bigint,target_dimension text,count_delta bigint,score_delta bigint
		) returns void as $$
		begin
			if target_route_id is null or target_dimension is null or (count_delta=0 and score_delta=0) then return; end if;
			insert into content_popularity_rating_dimension_facts(object_route_id,dimension_code)
			values(target_route_id,target_dimension) on conflict(object_route_id,dimension_code) do nothing;
			update content_popularity_rating_dimension_facts set
			rating_count=rating_count+count_delta,score_sum=score_sum+score_delta,updated_at=now()
			where object_route_id=target_route_id and dimension_code=target_dimension;
			delete from content_popularity_rating_dimension_facts
			where object_route_id=target_route_id and dimension_code=target_dimension and rating_count=0;
		end;
		$$ language plpgsql`,
		`create or replace function content_popularity_actor_eligible(target_route_id bigint,target_user_id bigint) returns boolean as $$
			select coalesce((select account.status='active' and account.security_score>=40
				and not content_target_user_is_developer(target_route_id,account.id)
				from users account where account.id=target_user_id),false)
		$$ language sql stable`,
		`create or replace function content_popularity_unique_view_eligible(target_route_id bigint,target_user_id bigint) returns boolean as $$
			select target_user_id is null or not content_target_user_is_developer(target_route_id,target_user_id)
		$$ language sql stable`,
		`create or replace function sync_content_view_popularity_fact() returns trigger as $$
		begin
			if tg_op='INSERT' then
				perform adjust_content_popularity_view_total(new.object_route_id,new.counter_shard,new.views);
			elsif tg_op='DELETE' then
				perform adjust_content_popularity_view_total(old.object_route_id,old.counter_shard,-old.views);
			elsif old.object_route_id=new.object_route_id and old.counter_shard=new.counter_shard then
				perform adjust_content_popularity_view_total(new.object_route_id,new.counter_shard,new.views-old.views);
			else
				perform adjust_content_popularity_view_total(old.object_route_id,old.counter_shard,-old.views);
				perform adjust_content_popularity_view_total(new.object_route_id,new.counter_shard,new.views);
			end if;
			if tg_op='DELETE' then return old; end if; return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_content_view_daily_popularity_facts
			after insert or update of views or delete on content_view_daily
			for each row execute function sync_content_view_popularity_fact()`,
		`create or replace function sync_content_unique_view_popularity_fact() returns trigger as $$
		declare old_count bigint:=0; new_count bigint:=0;
		begin
			if tg_op<>'INSERT' and content_popularity_unique_view_eligible(old.object_route_id,old.viewer_user_id) then old_count:=1; end if;
			if tg_op<>'DELETE' and content_popularity_unique_view_eligible(new.object_route_id,new.viewer_user_id) then new_count:=1; end if;
			if tg_op<>'INSERT' then perform adjust_content_popularity_lifetime_facts(old.object_route_id,-old_count,0,0,0,0,0,0,0); end if;
			if tg_op<>'DELETE' then perform adjust_content_popularity_lifetime_facts(new.object_route_id,new_count,0,0,0,0,0,0,0); end if;
			if tg_op='DELETE' then return old; end if; return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_content_unique_views_popularity_facts
			after insert or update of viewer_user_id or delete on content_unique_views
			for each row execute function sync_content_unique_view_popularity_fact()`,
		`create or replace function sync_content_project_page_popularity_fact() returns trigger as $$
		begin
			perform adjust_content_popularity_lifetime_facts(
				case when tg_op='DELETE' then old.object_route_id else new.object_route_id end,
				0,case when tg_op='DELETE' then -1 else 1 end,0,0,0,0,0,0);
			if tg_op='DELETE' then return old; end if; return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_content_project_pages_popularity_facts
			after insert or delete on content_project_pages
			for each row execute function sync_content_project_page_popularity_fact()`,
		`create or replace function sync_content_download_popularity_fact() returns trigger as $$
		begin
			if tg_op='INSERT' then
				perform adjust_content_popularity_lifetime_facts(new.object_route_id,0,0,new.downloads,0,0,0,0,0);
			elsif tg_op='DELETE' then
				perform adjust_content_popularity_lifetime_facts(old.object_route_id,0,0,-old.downloads,0,0,0,0,0);
			elsif old.object_route_id=new.object_route_id then
				perform adjust_content_popularity_lifetime_facts(new.object_route_id,0,0,new.downloads-old.downloads,0,0,0,0,0);
			else
				perform adjust_content_popularity_lifetime_facts(old.object_route_id,0,0,-old.downloads,0,0,0,0,0);
				perform adjust_content_popularity_lifetime_facts(new.object_route_id,0,0,new.downloads,0,0,0,0,0);
			end if;
			if tg_op='DELETE' then return old; end if; return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_content_download_counters_popularity_facts
			after insert or update of downloads or delete on content_download_counters
			for each row execute function sync_content_download_popularity_fact()`,
		`create or replace function adjust_content_popularity_rating_dimensions(target_route_id bigint,target_rating_id bigint,multiplier bigint) returns void as $$
		declare item record;
		begin
			for item in select dimension_code,score from content_rating_scores where rating_id=target_rating_id loop
				perform adjust_content_popularity_dimension_fact(target_route_id,item.dimension_code,multiplier,multiplier*item.score);
			end loop;
		end;
		$$ language plpgsql`,
		`create or replace function sync_content_rating_score_popularity_fact() returns trigger as $$
		declare route_id bigint; eligible boolean;
		begin
			if tg_op='DELETE' and pg_trigger_depth()>1 then return old; end if;
			if tg_op<>'INSERT' then
				select rating.object_route_id,content_popularity_actor_eligible(rating.object_route_id,rating.author_id) and rating.status='published'
				into route_id,eligible from content_ratings rating where rating.id=old.rating_id;
				if eligible then perform adjust_content_popularity_dimension_fact(route_id,old.dimension_code,-1,-old.score); end if;
			end if;
			if tg_op<>'DELETE' then
				select rating.object_route_id,content_popularity_actor_eligible(rating.object_route_id,rating.author_id) and rating.status='published'
				into route_id,eligible from content_ratings rating where rating.id=new.rating_id;
				if eligible then perform adjust_content_popularity_dimension_fact(route_id,new.dimension_code,1,new.score); end if;
			end if;
			if tg_op='DELETE' then return old; end if; return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_content_rating_scores_popularity_facts
			after insert or update or delete on content_rating_scores
			for each row execute function sync_content_rating_score_popularity_fact()`,
		popularityLifetimeRebuildFunctionStatement(),
	}
}

func popularityLifetimeRebuildFunctionStatement() string {
	return `create or replace function rebuild_content_popularity_lifetime_facts(target_route_id bigint) returns void as $$
	declare
		route_type text; route_internal_id bigint;
		unique_views bigint; pages bigint; downloads bigint; favorites bigint;
		comment_total bigint; commenter_total bigint; rating_total bigint; rating_score_total bigint;
	begin
		select entity_type,internal_id into route_type,route_internal_id from public_routes where id=target_route_id;
		if not found then return; end if;

		delete from content_popularity_view_totals where object_route_id=target_route_id;
		insert into content_popularity_view_totals(object_route_id,counter_shard,views,updated_at)
		select target_route_id,counter_shard,sum(views),now() from content_view_daily
		where object_route_id=target_route_id group by counter_shard;
		select count(*) into unique_views from content_unique_views
		where object_route_id=target_route_id and (viewer_user_id is null or not exists(
			select 1 from effective_project_access access where access.project_type=route_type
			and access.project_id=route_internal_id and access.access_level='developer' and access.user_id=viewer_user_id));
		select count(*) into pages from content_project_pages where object_route_id=target_route_id;
		select coalesce(sum(counter.downloads),0) into downloads from content_download_counters counter where counter.object_route_id=target_route_id;
		select count(distinct collection.user_id) into favorites
		from favorite_collection_items item join favorite_collections collection on collection.id=item.collection_id
		join users actor on actor.id=collection.user_id
		where item.entity_type=route_type and item.entity_id=route_internal_id
		and not exists(select 1 from effective_project_access access where access.project_type=route_type
			and access.project_id=route_internal_id and access.access_level='developer' and access.user_id=collection.user_id)
		and actor.status='active' and actor.security_score>=40;
		select count(*),count(distinct author_id) into comment_total,commenter_total from (
			select comment.author_id from comments comment join users actor on actor.id=comment.author_id
			where comment.status='published' and comment.target_type=route_type and comment.target_id=route_internal_id
			and not exists(select 1 from effective_project_access access where access.project_type=route_type
				and access.project_id=route_internal_id and access.access_level='developer' and access.user_id=comment.author_id)
			and actor.status='active' and actor.security_score>=40
			union all
			select comment.author_id from comments comment join users actor on actor.id=comment.author_id
			join mod_content_versions version on version.id=comment.target_version_id and version.mod_id=route_internal_id
			where route_type='mod' and comment.status='published' and comment.target_type='mod_resource'
			and not exists(select 1 from effective_project_access access where access.project_type=route_type
				and access.project_id=route_internal_id and access.access_level='developer' and access.user_id=comment.author_id)
			and actor.status='active' and actor.security_score>=40
		) eligible_comments;
		select count(*),coalesce(sum(rating.overall_score),0) into rating_total,rating_score_total
		from content_ratings rating join users actor on actor.id=rating.author_id
		where rating.object_route_id=target_route_id and rating.status='published'
		and not exists(select 1 from effective_project_access access where access.project_type=route_type
			and access.project_id=route_internal_id and access.access_level='developer' and access.user_id=rating.author_id)
		and actor.status='active' and actor.security_score>=40;

		insert into content_popularity_lifetime_facts(
			object_route_id,unique_view_count,page_count,download_count,favorite_count,
			comment_count,effective_commenter_count,rating_count,rating_sum,calibrated_at,updated_at
		) values(target_route_id,unique_views,pages,downloads,favorites,comment_total,commenter_total,
			rating_total,rating_score_total,now(),now())
		on conflict(object_route_id) do update set unique_view_count=excluded.unique_view_count,
			page_count=excluded.page_count,download_count=excluded.download_count,favorite_count=excluded.favorite_count,
			comment_count=excluded.comment_count,effective_commenter_count=excluded.effective_commenter_count,
			rating_count=excluded.rating_count,rating_sum=excluded.rating_sum,calibrated_at=now(),updated_at=now();
		delete from content_popularity_rating_dimension_facts where object_route_id=target_route_id;
		insert into content_popularity_rating_dimension_facts(object_route_id,dimension_code,rating_count,score_sum)
		select target_route_id,score.dimension_code,count(*),sum(score.score)
		from content_rating_scores score join content_ratings rating on rating.id=score.rating_id
		join users actor on actor.id=rating.author_id
		where rating.object_route_id=target_route_id and rating.status='published'
		and not exists(select 1 from effective_project_access access where access.project_type=route_type
			and access.project_id=route_internal_id and access.access_level='developer' and access.user_id=rating.author_id)
		and actor.status='active' and actor.security_score>=40
		group by score.dimension_code;
	end;
	$$ language plpgsql`
}
