package database

// The same actor can post multiple comments and favorite a project in several
// collections. Keep one contribution receipt per actor/project so removal and
// reclassification can reverse exactly what was applied to the lifetime facts.
func socialPopularitySchemaStatements() []string {
	return []string{
		`create table content_social_contributions (
			object_route_id bigint not null references public_routes(id) on delete cascade,
			actor_id bigint not null references users(id) on delete cascade,
			published_comments bigint not null default 0 check(published_comments>=0),
			favorite_memberships bigint not null default 0 check(favorite_memberships>=0),
			counted boolean not null default false,
			comment_weight numeric not null default 0 check(comment_weight>=0),
			favorite_weight numeric not null default 0 check(favorite_weight>=0),
			primary key(object_route_id,actor_id)
		)`,
		`create index idx_content_social_contributions_actor on content_social_contributions(actor_id,object_route_id)`,
		`create function apply_social_contribution() returns trigger as $$
		declare
			comments_delta bigint:=0; commenters_delta bigint:=0; favorites_delta bigint:=0;
			comment_trend numeric:=-old.comment_weight; favorite_trend numeric:=-old.favorite_weight;
		begin
			if tg_op='DELETE' and not exists(select 1 from public_routes where id=old.object_route_id) then return null; end if;
			if old.counted then
				comments_delta:=-old.published_comments;
				commenters_delta:=-(old.published_comments>0)::int;
				favorites_delta:=-(old.favorite_memberships>0)::int;
			end if;
			if tg_op='UPDATE' then
				if new.counted then
					comments_delta:=comments_delta+new.published_comments;
					commenters_delta:=commenters_delta+(new.published_comments>0)::int;
					favorites_delta:=favorites_delta+(new.favorite_memberships>0)::int;
				end if;
				comment_trend:=comment_trend+new.comment_weight;
				favorite_trend:=favorite_trend+new.favorite_weight;
			end if;
			if (comments_delta,commenters_delta,favorites_delta)<>(0,0,0) then
				perform adjust_content_popularity_lifetime_facts(old.object_route_id,0,0,0,favorites_delta,comments_delta,commenters_delta,0,0);
			end if;
			if comment_trend<>0 then perform record_popularity_event(old.object_route_id,'comment',comment_trend); end if;
			if favorite_trend<>0 then perform record_popularity_event(old.object_route_id,'favorite',favorite_trend); end if;
			if (comments_delta,commenters_delta,favorites_delta,comment_trend,favorite_trend)<>(0,0,0,0,0) then
				perform enqueue_content_stats_refresh(old.object_route_id,true,true);
			end if;
			return null;
		end;
		$$ language plpgsql`,
		`create trigger trg_content_social_projection after update or delete on content_social_contributions
			for each row execute function apply_social_contribution()`,
		`create function adjust_social_contribution(target_route bigint,target_actor bigint,comment_delta bigint,favorite_delta bigint)
		returns void as $$
		declare
			receipt content_social_contributions%rowtype;
			comments_next bigint; favorites_next bigint; weight numeric;
		begin
			-- Coordinate first interactions with invalidations, even when the
			-- receipt does not exist yet. Cascading deletion handles its own row.
			perform 1 from users where id=target_actor for share;
			if not found then return; end if;
			perform 1 from public_routes where id=target_route for share;
			if not found then return; end if;
			insert into content_social_contributions(object_route_id,actor_id)
			values(target_route,target_actor) on conflict do nothing;
			select * into strict receipt from content_social_contributions
				where object_route_id=target_route and actor_id=target_actor for update;
			comments_next:=receipt.published_comments+comment_delta;
			favorites_next:=receipt.favorite_memberships+favorite_delta;
			if comments_next=0 and favorites_next=0 then
				delete from content_social_contributions where object_route_id=target_route and actor_id=target_actor;
				return;
			end if;
			if (receipt.published_comments=0 and comments_next>0) or (receipt.favorite_memberships=0 and favorites_next>0) then
				weight:=case when content_target_user_is_developer(target_route,target_actor) then 0 else coalesce(content_user_trust(target_actor),0.2) end;
			end if;
			update content_social_contributions set published_comments=comments_next,favorite_memberships=favorites_next,
				counted=content_popularity_actor_eligible(target_route,target_actor),
				comment_weight=case when comments_next=0 then 0 when published_comments=0 then 3*weight else comment_weight end,
				favorite_weight=case when favorites_next=0 then 0 when favorite_memberships=0 then 4*weight else favorite_weight end
				where object_route_id=target_route and actor_id=target_actor;
		end;
		$$ language plpgsql`,
		`create function reclassify_social_contributions(affected_user bigint,affected_route bigint) returns void as $$
		begin
			if affected_user is null and affected_route is null then raise exception 'missing social contribution scope'; end if;
			perform 1 from content_social_contributions
			where (affected_user is null or actor_id=affected_user) and (affected_route is null or object_route_id=affected_route)
			order by object_route_id,actor_id for update;
			update content_social_contributions set counted=not counted
			where (affected_user is null or actor_id=affected_user) and (affected_route is null or object_route_id=affected_route)
				and counted is distinct from content_popularity_actor_eligible(object_route_id,actor_id);
		end;
		$$ language plpgsql`,
		`create function remove_collection_social_contributions() returns trigger as $$
		begin
			-- A cascading delete otherwise loses the collection owner before
			-- the item trigger can attribute the removal.
			delete from favorite_collection_items where collection_id=old.id;
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_favorite_collections_detach_items before delete on favorite_collections
			for each row execute function remove_collection_social_contributions()`,
	}
}

func favoritePopularityFunctionStatement() string {
	return `create or replace function refresh_popularity_from_favorite() returns trigger as $$
	declare
		item favorite_collection_items%rowtype;
		route_id bigint; actor bigint; change bigint;
	begin
		if tg_op='DELETE' then item:=old; change:=-1; else item:=new; change:=1; end if;
		select id into route_id from public_routes where entity_type=item.entity_type and internal_id=item.entity_id
			and entity_type in ('mod','modpack','plugin','map','resource_pack','shader_pack','datapack','addon','minecraft_server');
		if route_id is null then return null; end if;
		select user_id into actor from favorite_collections where id=item.collection_id;
		if found then perform adjust_social_contribution(route_id,actor,0,change); end if;
		return null;
	end;
	$$ language plpgsql`
}
