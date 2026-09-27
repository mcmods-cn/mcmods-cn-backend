package database

import "strings"

// ratingSchemaStatements installs the shared rating and heat model for every
// top-level project family. Public IDs are resolved at the HTTP boundary; all
// persisted relationships use numeric route and user IDs.
func ratingSchemaStatements() []string {
	statements := []string{
		`create table content_ratings (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			object_route_id bigint not null references public_routes(id) on delete cascade,
			author_id bigint not null references users(id) on delete cascade,
			overall_score smallint not null check(overall_score between 1 and 5),
			message text not null default '' check(char_length(message) <= 2000),
			status text not null default 'published' check(status in ('published','hidden')),
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			unique(object_route_id,author_id)
		)`,
		`create index idx_content_ratings_target
			on content_ratings(object_route_id,status,updated_at desc,id desc)`,
		`create index idx_content_ratings_author
			on content_ratings(author_id,updated_at desc,id desc)`,
		`create table content_rating_scores (
			rating_id bigint not null references content_ratings(id) on delete cascade,
			dimension_code text not null check(dimension_code ~ '^[a-z][a-z0-9_]{1,63}$'),
			score smallint not null check(score between 1 and 5),
			primary key(rating_id,dimension_code)
		)`,
		`create index idx_content_rating_scores_dimension
			on content_rating_scores(dimension_code,rating_id)`,
		`create table content_view_daily (
			object_route_id bigint not null references public_routes(id) on delete cascade,
			view_date date not null default current_date,
			counter_shard smallint not null default 0 check(counter_shard between 0 and 31),
			views bigint not null default 0 check(views >= 0),
			primary key(object_route_id,view_date,counter_shard)
		)`,
		`create index idx_content_view_daily_retention
			on content_view_daily(view_date,object_route_id)`,
		`create table content_unique_views (
			object_route_id bigint not null references public_routes(id) on delete cascade,
			viewer_hash bytea not null,
			viewer_user_id bigint references users(id) on delete set null,
			first_seen_at timestamptz not null default now(),
			last_seen_at timestamptz not null default now(),
			primary key(object_route_id,viewer_hash)
		)`,
		`create index idx_content_unique_views_user
			on content_unique_views(viewer_user_id,object_route_id) where viewer_user_id is not null`,
		`create table content_project_pages (
			object_route_id bigint not null references public_routes(id) on delete cascade,
			page_hash bytea not null,
			first_seen_at timestamptz not null default now(),
			primary key(object_route_id,page_hash)
		)`,
		`create table content_popularity_thresholds (
			entity_type text primary key,
			favorite_threshold numeric not null check(favorite_threshold>0),
			commenter_threshold numeric not null check(commenter_threshold>0),
			download_threshold numeric not null check(download_threshold>0),
			rating_threshold numeric not null check(rating_threshold>0),
			view_threshold numeric not null check(view_threshold>0),
			trend_threshold numeric not null check(trend_threshold>0)
		)`,
		`insert into content_popularity_thresholds values
			('mod',1000,300,20000,500,5000,100),
			('modpack',500,150,10000,250,2500,75),
			('plugin',500,150,10000,250,2500,75),
			('map',400,120,8000,200,2000,60),
			('resource_pack',400,120,8000,200,2000,60),
			('shader_pack',400,120,8000,200,2000,60),
			('datapack',400,120,8000,200,2000,60),
			('addon',400,120,8000,200,2000,60),
			('minecraft_server',500,200,1,300,5000,80)`,
		`create table content_popularity_events_daily (
			object_route_id bigint not null references public_routes(id) on delete cascade,
			event_date date not null default current_date,
			favorite_value numeric not null default 0,
			comment_value numeric not null default 0,
			rating_value numeric not null default 0,
			release_value numeric not null default 0,
			primary key(object_route_id,event_date)
		)`,
		`create index idx_content_popularity_events_retention
			on content_popularity_events_daily(event_date,object_route_id)`,
		`create table content_heat_promotion_counters (
			object_route_id bigint primary key references public_routes(id) on delete cascade,
			last_sequence_no bigint not null check(last_sequence_no>0)
		)`,
		`create table content_heat_promotions (
			id bigserial primary key,
			object_route_id bigint not null references public_routes(id) on delete cascade,
			shop_item_id bigint not null references shop_items(id) on delete restrict,
			applied_by bigint not null references users(id) on delete restrict,
			promotion_kind text not null check(promotion_kind in ('project','server')),
			base_power numeric(10,4) not null check(base_power>0),
			effective_power numeric(10,4) not null check(effective_power>0),
			half_life_hours integer not null check(half_life_hours between 1 and 8760),
			sequence_no bigint not null check(sequence_no>0),
			started_at timestamptz not null default now(),
			expires_at timestamptz not null,
			check(expires_at>started_at),
			unique(object_route_id,sequence_no)
		)`,
		`create index idx_content_heat_promotions_active
			on content_heat_promotions(object_route_id,expires_at desc,started_at desc)`,
		`create index idx_content_heat_promotions_user
			on content_heat_promotions(applied_by,started_at desc,id desc)`,
		`create table content_popularity_stats (
			object_route_id bigint primary key references public_routes(id) on delete cascade,
			view_count bigint not null default 0 check(view_count>=0),
			unique_view_count bigint not null default 0 check(unique_view_count>=0),
			page_count integer not null default 1 check(page_count>0),
			download_count bigint not null default 0 check(download_count>=0),
			favorite_count bigint not null default 0 check(favorite_count>=0),
			comment_count bigint not null default 0 check(comment_count>=0),
			effective_commenter_count bigint not null default 0 check(effective_commenter_count>=0),
			rating_count bigint not null default 0 check(rating_count>=0),
			rating_sum bigint not null default 0 check(rating_sum>=0),
			rating_average numeric(6,4) not null default 0 check(rating_average between 0 and 5),
			bayesian_rating numeric(6,4) not null default 0 check(bayesian_rating between 0 and 5),
			dimension_averages jsonb not null default '{}'::jsonb,
			long_term_score numeric(10,6) not null default 0,
			trend_score numeric(10,6) not null default 0,
			effective_view_score numeric(10,6) not null default 0,
			promotion_score numeric(10,6) not null default 0,
			quality_modifier numeric(10,6) not null default 1,
			new_project_boost numeric(10,6) not null default 1,
			heat_score numeric(16,6) not null default 0 check(heat_score>=0),
			decay_until timestamptz,
			next_decay_at timestamptz,
			updated_at timestamptz not null default now()
		)`,
		`create index idx_content_popularity_heat
			on content_popularity_stats(heat_score desc,object_route_id)`,
		`create index idx_content_popularity_rating
			on content_popularity_stats(bayesian_rating desc,rating_count desc,object_route_id)`,
		`create index idx_content_popularity_stats_decay_due
			on content_popularity_stats(next_decay_at,object_route_id) where next_decay_at is not null`,
		`create table content_popularity_daily_snapshots (
			object_route_id bigint not null references public_routes(id) on delete cascade,
			metric_date date not null default current_date,
			heat_score numeric(16,6) not null default 0 check(heat_score>=0),
			view_count bigint not null default 0 check(view_count>=0),
			download_count bigint not null default 0 check(download_count>=0),
			favorite_count bigint not null default 0 check(favorite_count>=0),
			comment_count bigint not null default 0 check(comment_count>=0),
			rating_count bigint not null default 0 check(rating_count>=0),
			updated_at timestamptz not null default now(),
			primary key(object_route_id,metric_date)
		)`,
		`create index idx_content_popularity_snapshots_date
			on content_popularity_daily_snapshots(metric_date,object_route_id)`,
		`create table content_rating_global_stats (
			entity_type text primary key,
			rating_count bigint not null default 0 check(rating_count>=0),
			rating_sum bigint not null default 0 check(rating_sum>=0),
			average_rating numeric(6,4) not null default 3.5 check(average_rating between 0 and 5),
			updated_at timestamptz not null default '-infinity'
		)`,
		`insert into content_rating_global_stats(entity_type)
			select entity_type from content_popularity_thresholds`,
		`create or replace function adjust_content_rating_global_stats(
			target_type text,count_delta bigint,sum_delta bigint
		) returns void as $$
		begin
			if target_type is null or (count_delta=0 and sum_delta=0) then return; end if;
			insert into content_rating_global_stats(entity_type) values(target_type)
			on conflict(entity_type) do nothing;
			update content_rating_global_stats set
				average_rating=case when rating_count+count_delta=0 then 3.5
					else (rating_sum+sum_delta)::numeric/(rating_count+count_delta) end,
				rating_count=rating_count+count_delta,
				rating_sum=rating_sum+sum_delta,
				updated_at=now()
			where entity_type=target_type;
		end;
		$$ language plpgsql`,
		`create or replace function content_target_user_is_developer(target_route_id bigint,target_user_id bigint) returns boolean as $$
		declare result boolean;
		begin
			if target_user_id is null then return false; end if;
			select exists(
				select 1 from public_routes route join effective_project_access access
				on access.project_type=route.entity_type and access.project_id=route.internal_id
				where route.id=target_route_id and access.access_level='developer' and access.user_id=target_user_id
			) into result;
			return coalesce(result,false);
		end;
		$$ language plpgsql stable`,
		`create or replace function content_target_created_at(target_route_id bigint) returns timestamptz as $$
		declare target_type text; target_id bigint; result timestamptz;
		begin
			select entity_type,internal_id into target_type,target_id from public_routes where id=target_route_id;
			case target_type
				when 'mod' then select created_at into result from mods where id=target_id;
				when 'modpack' then select created_at into result from modpacks where id=target_id;
				when 'minecraft_server' then select created_at into result from minecraft_servers where id=target_id;
				else select created_at into result from simple_projects where id=target_id and project_type=target_type;
			end case;
			return result;
		end;
		$$ language plpgsql stable`,
		`create or replace function normalized_popularity(value numeric,threshold_value numeric) returns numeric as $$
			select least(1,greatest(0,ln(1+greatest(value,0))/ln(1+greatest(threshold_value,1))))
		$$ language sql immutable strict`,
		`create or replace function content_user_trust(target_user_id bigint) returns numeric as $$
			select case
				when account.status<>'active' or account.security_score<40 then 0.2
				when account.created_at>now()-interval '30 days' then 0.5
				when coalesce(experience.level,0)>=5 or (select count(*) from user_activity_events event
					where event.user_id=account.id and event.occurred_at>=now()-interval '90 days')>=20 then 1.2
				else 1 end
			from users account left join user_experience experience on experience.user_id=account.id
			where account.id=target_user_id
		$$ language sql stable strict`,
	}
	statements = append(statements, popularityFactProjectionStatements()...)
	return append(statements, []string{
		`create or replace function record_popularity_event(target_route_id bigint,event_kind text,event_value numeric) returns void as $$
		begin
			insert into content_popularity_events_daily(
				object_route_id,event_date,favorite_value,comment_value,rating_value,release_value
			) values(target_route_id,current_date,
				case when event_kind='favorite' then event_value else 0 end,
				case when event_kind='comment' then event_value else 0 end,
				case when event_kind='rating' then event_value else 0 end,
				case when event_kind='release' then event_value else 0 end)
			on conflict(object_route_id,event_date) do update set
				favorite_value=content_popularity_events_daily.favorite_value+excluded.favorite_value,
				comment_value=content_popularity_events_daily.comment_value+excluded.comment_value,
				rating_value=content_popularity_events_daily.rating_value+excluded.rating_value,
				release_value=content_popularity_events_daily.release_value+excluded.release_value;
		end;
		$$ language plpgsql`,
		contentRouteForCommentFunctionStatement(),
		contentRatingGlobalRebuildFunctionStatement(),
		`create or replace function refresh_content_popularity(target_route_id bigint) returns void as $$
		declare
			route_type text; route_internal_id bigint; route_created_at timestamptz;
			total_views bigint; total_unique_views bigint; total_pages integer; total_downloads bigint;
			total_favorites bigint; total_comments bigint; total_commenters bigint; total_ratings bigint; total_rating_score bigint;
			route_average_rating numeric; global_average numeric; bayesian numeric; averages jsonb;
			favorite_limit numeric; commenter_limit numeric; download_limit numeric;
			rating_limit numeric; view_limit numeric; trend_limit numeric;
			favorite_score numeric; commenter_score numeric; download_score numeric; rating_score numeric;
			long_term numeric; trend_raw numeric; trend numeric; effective_views numeric;
			promotion numeric; quality numeric; new_boost numeric; calculated_heat numeric;
			latest_event_date date; promotion_expires timestamptz; calculated_decay_until timestamptz; calculated_next_decay timestamptz;
		begin
			select route.entity_type,route.internal_id into route_type,route_internal_id
			from public_routes route where route.id=target_route_id;
			if not found or route_type not in ('mod','modpack','plugin','map','resource_pack','shader_pack','datapack','addon','minecraft_server') then return; end if;
			route_created_at:=coalesce(content_target_created_at(target_route_id),now());
			select threshold.favorite_threshold,threshold.commenter_threshold,threshold.download_threshold,
				threshold.rating_threshold,threshold.view_threshold,threshold.trend_threshold
			into favorite_limit,commenter_limit,download_limit,rating_limit,view_limit,trend_limit
			from content_popularity_thresholds threshold where threshold.entity_type=route_type;

			select coalesce(sum(view_total.views),0) into total_views
			from content_popularity_view_totals view_total where view_total.object_route_id=target_route_id;
			select coalesce(facts.unique_view_count,0),greatest(1,coalesce(facts.page_count,0)),
				coalesce(facts.download_count,0),coalesce(facts.favorite_count,0),coalesce(facts.comment_count,0),
				coalesce(facts.effective_commenter_count,0),coalesce(facts.rating_count,0),coalesce(facts.rating_sum,0)
			into total_unique_views,total_pages,total_downloads,total_favorites,total_comments,total_commenters,total_ratings,total_rating_score
			from content_popularity_lifetime_facts facts where facts.object_route_id=target_route_id;
			if not found then
				total_unique_views:=0; total_pages:=1; total_downloads:=0; total_favorites:=0;
				total_comments:=0; total_commenters:=0; total_ratings:=0; total_rating_score:=0;
			end if;
			route_average_rating:=case when total_ratings=0 then 0 else total_rating_score::numeric/total_ratings end;
			select coalesce((select global_stats.average_rating from content_rating_global_stats global_stats where global_stats.entity_type=route_type),3.5)
			into global_average;
			bayesian:=case when total_ratings=0 then global_average else
				(route_average_rating*total_ratings+global_average*10)/(total_ratings+10) end;
			select coalesce(jsonb_object_agg(dimension_code,round(score_sum::numeric/rating_count,2)),'{}'::jsonb)
			into averages from content_popularity_rating_dimension_facts
			where object_route_id=target_route_id and rating_count>0;

			favorite_score:=normalized_popularity(total_favorites,favorite_limit);
			commenter_score:=normalized_popularity(total_commenters,commenter_limit);
			download_score:=normalized_popularity(total_downloads,download_limit);
			rating_score:=normalized_popularity(total_ratings,rating_limit);
			long_term:=case when route_type='minecraft_server'
				then 0.50*favorite_score+0.30*commenter_score+0.20*rating_score
				else 0.40*favorite_score+0.25*commenter_score+0.20*download_score+0.15*rating_score end;
			select coalesce(sum((favorite_value+comment_value+rating_value+release_value)*
				power(2::numeric,-(current_date-event_date)::numeric/7)),0),max(event_date) into trend_raw,latest_event_date
			from content_popularity_events_daily where object_route_id=target_route_id and event_date>=current_date-90;
			trend:=least(1,greatest(0,trend_raw)/trend_limit);
			effective_views:=normalized_popularity(total_unique_views,view_limit)/power(greatest(total_pages,1)::numeric,0.3);
			select least(1.5,coalesce(sum(effective_power*power(2::numeric,
				-extract(epoch from (now()-started_at))/(3600.0*half_life_hours))),0)),max(expires_at) into promotion,promotion_expires
			from content_heat_promotions where object_route_id=target_route_id and expires_at>now();
			quality:=0.85+0.15*least(1,greatest(0,(bayesian-1)/4));
			new_boost:=1+0.15*power(2::numeric,-greatest(0,extract(epoch from (now()-route_created_at))/86400.0)/14);
			calculated_heat:=100*(0.45*long_term+0.35*trend+0.10*effective_views+0.10*promotion)*quality*new_boost;
			calculated_decay_until:=greatest(route_created_at+interval '60 days',(latest_event_date+91)::timestamptz,promotion_expires);
			calculated_next_decay:=case when calculated_decay_until>now() then now()+interval '15 minutes' else null end;

			insert into content_popularity_stats(
				object_route_id,view_count,unique_view_count,page_count,download_count,favorite_count,comment_count,effective_commenter_count,
				rating_count,rating_sum,rating_average,bayesian_rating,dimension_averages,long_term_score,
				trend_score,effective_view_score,promotion_score,quality_modifier,new_project_boost,heat_score,decay_until,next_decay_at,updated_at
			) values(target_route_id,total_views,total_unique_views,total_pages,total_downloads,total_favorites,total_comments,total_commenters,
				total_ratings,total_rating_score,route_average_rating,bayesian,averages,long_term,trend,effective_views,promotion,
				quality,new_boost,greatest(0,calculated_heat),calculated_decay_until,calculated_next_decay,now())
			on conflict(object_route_id) do update set
				view_count=excluded.view_count,unique_view_count=excluded.unique_view_count,page_count=excluded.page_count,
				download_count=excluded.download_count,favorite_count=excluded.favorite_count,comment_count=excluded.comment_count,
				effective_commenter_count=excluded.effective_commenter_count,
				rating_count=excluded.rating_count,rating_sum=excluded.rating_sum,rating_average=excluded.rating_average,
				bayesian_rating=excluded.bayesian_rating,dimension_averages=excluded.dimension_averages,
				long_term_score=excluded.long_term_score,trend_score=excluded.trend_score,
				effective_view_score=excluded.effective_view_score,promotion_score=excluded.promotion_score,
				quality_modifier=excluded.quality_modifier,new_project_boost=excluded.new_project_boost,
				heat_score=excluded.heat_score,decay_until=excluded.decay_until,next_decay_at=excluded.next_decay_at,updated_at=now();
			insert into content_popularity_daily_snapshots(
				object_route_id,metric_date,heat_score,view_count,download_count,favorite_count,comment_count,rating_count,updated_at
			) values(target_route_id,current_date,greatest(0,calculated_heat),total_views,total_downloads,
				total_favorites,total_comments,total_ratings,now())
			on conflict(object_route_id,metric_date) do update set heat_score=excluded.heat_score,
				view_count=excluded.view_count,download_count=excluded.download_count,
				favorite_count=excluded.favorite_count,comment_count=excluded.comment_count,
				rating_count=excluded.rating_count,updated_at=now();
		end;
		$$ language plpgsql`,
		`create or replace function refresh_popularity_from_rating() returns trigger as $$
		declare
			route_id bigint; trust numeric; old_published boolean; new_published boolean;
			old_metric boolean:=false; new_metric boolean:=false;
			old_route_type text; new_route_type text;
		begin
			route_id:=case when tg_op='DELETE' then old.object_route_id else new.object_route_id end;
			if tg_op<>'INSERT' then select entity_type into old_route_type from public_routes where id=old.object_route_id; end if;
			if tg_op<>'DELETE' then select entity_type into new_route_type from public_routes where id=new.object_route_id; end if;
			trust:=coalesce(content_user_trust(case when tg_op='DELETE' then old.author_id else new.author_id end),0.2);
			old_published:=tg_op<>'INSERT' and old.status='published'
				and not content_target_user_is_developer(old.object_route_id,old.author_id);
			new_published:=tg_op<>'DELETE' and new.status='published'
				and not content_target_user_is_developer(new.object_route_id,new.author_id);
			old_metric:=tg_op<>'INSERT' and old.status='published' and content_popularity_actor_eligible(old.object_route_id,old.author_id);
			new_metric:=tg_op<>'DELETE' and new.status='published' and content_popularity_actor_eligible(new.object_route_id,new.author_id);
			if old_metric and new_metric and old.object_route_id=new.object_route_id then
				perform adjust_content_popularity_lifetime_facts(
					new.object_route_id,0,0,0,0,0,0,0,new.overall_score-old.overall_score);
			elsif old_metric then
				perform adjust_content_popularity_lifetime_facts(old.object_route_id,0,0,0,0,0,0,-1,-old.overall_score);
				if new_metric then
					perform adjust_content_popularity_lifetime_facts(new.object_route_id,0,0,0,0,0,0,1,new.overall_score);
				end if;
			elsif new_metric then
				perform adjust_content_popularity_lifetime_facts(new.object_route_id,0,0,0,0,0,0,1,new.overall_score);
			end if;
			if old_metric and new_metric and old_route_type=new_route_type then
				perform adjust_content_rating_global_stats(new_route_type,0,new.overall_score-old.overall_score);
			elsif old_metric then
				perform adjust_content_rating_global_stats(old_route_type,-1,-old.overall_score);
				if new_metric then
					perform adjust_content_rating_global_stats(new_route_type,1,new.overall_score);
				end if;
			elsif new_metric then
				perform adjust_content_rating_global_stats(new_route_type,1,new.overall_score);
			end if;
			if old_metric and (not new_metric or old.object_route_id is distinct from new.object_route_id) then
				perform adjust_content_popularity_rating_dimensions(old.object_route_id,old.id,-1);
			end if;
			if new_metric and (not old_metric or old.object_route_id is distinct from new.object_route_id) then
				perform adjust_content_popularity_rating_dimensions(new.object_route_id,new.id,1);
			end if;
			if not old_published and new_published then perform record_popularity_event(route_id,'rating',2*trust);
			elsif old_published and not new_published then perform record_popularity_event(route_id,'rating',-2*trust); end if;
			perform enqueue_content_stats_refresh(route_id,true,true);
			if tg_op='DELETE' then return old; end if; return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_content_ratings_popularity before insert or update or delete on content_ratings
			for each row execute function refresh_popularity_from_rating()`,
		contentPopularityCommentRefreshFunctionStatement(),
		`create trigger trg_comments_popularity after insert or update of status or delete on comments
			for each row execute function refresh_popularity_from_comment()`,
		`create or replace function refresh_popularity_from_favorite() returns trigger as $$
		declare route_id bigint; actor_user_id bigint; remaining bigint; trust numeric; metric_eligible boolean;
		begin
			select route.id into route_id from public_routes route where
				route.entity_type=case when tg_op='DELETE' then old.entity_type else new.entity_type end and
				route.internal_id=case when tg_op='DELETE' then old.entity_id else new.entity_id end and
				route.entity_type in ('mod','modpack','plugin','map','resource_pack','shader_pack','datapack','addon','minecraft_server');
			if route_id is null then if tg_op='DELETE' then return old; end if; return new; end if;
			select collection.user_id into actor_user_id from favorite_collections collection
			where collection.id=case when tg_op='DELETE' then old.collection_id else new.collection_id end;
			trust:=coalesce(content_user_trust(actor_user_id),0.2);
			metric_eligible:=content_popularity_actor_eligible(route_id,actor_user_id);
			if not content_target_user_is_developer(route_id,actor_user_id) then
				select count(*) into remaining from favorite_collection_items item
				join favorite_collections collection on collection.id=item.collection_id
				where collection.user_id=actor_user_id and item.entity_type=(select entity_type from public_routes where id=route_id)
				and item.entity_id=(select internal_id from public_routes where id=route_id);
				if tg_op='INSERT' and remaining=1 then
					perform record_popularity_event(route_id,'favorite',4*trust);
					if metric_eligible then perform adjust_content_popularity_lifetime_facts(route_id,0,0,0,1,0,0,0,0); end if;
				elsif tg_op='DELETE' and remaining=0 then
					perform record_popularity_event(route_id,'favorite',-4*trust);
					if metric_eligible then perform adjust_content_popularity_lifetime_facts(route_id,0,0,0,-1,0,0,0,0); end if;
				end if;
			end if;
			perform enqueue_content_stats_refresh(route_id,true,true);
			if tg_op='DELETE' then return old; end if; return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_favorite_items_popularity after insert or delete on favorite_collection_items
			for each row execute function refresh_popularity_from_favorite()`,
	}...)
}

func contentRouteForCommentFunctionStatement() string {
	return `create or replace function content_route_for_comment(kind text,internal_id bigint,version_id bigint) returns bigint as $$
		select case when content_route_for_comment.kind='mod_resource' then (
			select route.id from mod_content_versions version
			join public_routes route on route.entity_type='mod' and route.internal_id=version.mod_id
			where version.id=content_route_for_comment.version_id
		) else (
			select route.id from public_routes route
			where route.entity_type=content_route_for_comment.kind
			  and route.internal_id=content_route_for_comment.internal_id
			  and route.entity_type in ('mod','modpack','plugin','map','resource_pack','shader_pack','datapack','addon','minecraft_server')
		) end
	$$ language sql stable`
}

func contentPopularityCommentRefreshFunctionStatement() string {
	return `create or replace function refresh_popularity_from_comment() returns trigger as $$
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
	$$ language plpgsql`
}

func contentPopularityRefreshFunctionStatement() string {
	const prefix = "create or replace function refresh_content_popularity("
	for _, statement := range ratingSchemaStatements() {
		if strings.HasPrefix(strings.TrimSpace(statement), prefix) {
			return statement
		}
	}
	panic("refresh_content_popularity schema statement is missing")
}

const contentRatingGlobalRebuildSQL = `with route_developers as (
	select route.id object_route_id,access.user_id
	from public_routes route
	join effective_project_access access
	  on access.project_type=route.entity_type and access.project_id=route.internal_id
	where route.entity_type=$1 and access.access_level='developer'
	group by route.id,access.user_id
), totals as (
	select count(*) rating_count,coalesce(sum(rating.overall_score),0) rating_sum,
		coalesce(avg(rating.overall_score),3.5) average_rating
	from content_ratings rating
	join public_routes route on route.id=rating.object_route_id and route.entity_type=$1
	join users actor on actor.id=rating.author_id
	where rating.status='published'
	  and not exists(select 1 from route_developers developer
		where developer.object_route_id=rating.object_route_id and developer.user_id=rating.author_id)
	  and actor.status='active' and actor.security_score>=40
)
insert into content_rating_global_stats(entity_type,rating_count,rating_sum,average_rating,updated_at)
select $1,rating_count,rating_sum,average_rating,now() from totals
on conflict(entity_type) do update set rating_count=excluded.rating_count,rating_sum=excluded.rating_sum,
	average_rating=excluded.average_rating,updated_at=excluded.updated_at`

func contentRatingGlobalRebuildFunctionStatement() string {
	return `create or replace function rebuild_content_rating_global_stats(target_type text) returns void as $$
	begin
	` + contentRatingGlobalRebuildSQL + `;
	end;
	$$ language plpgsql`
}
