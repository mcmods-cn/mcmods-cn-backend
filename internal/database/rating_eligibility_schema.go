package database

// A vote records whether its score actually reached the route, global and
// dimension facts. Removing that contribution must use the old receipt. User
// and project changes reclassify both ratings and social contributions.
func ratingEligibilityStatements() []string {
	return []string{
		`create function classify_rating() returns trigger as $$
		begin
			if tg_op='INSERT' then
				-- A new vote has no existing row for either invalidator to lock.
				perform 1 from users where id=new.author_id for share;
				perform 1 from public_routes where id=new.object_route_id for share;
			elsif (old.author_id,old.object_route_id)<>(new.author_id,new.object_route_id) then
				raise exception 'rating author and project are immutable' using errcode='23514';
			end if;
			if new.status<>'published' then new.trend_weight:=0;
			elsif tg_op='UPDATE' and old.status='published' and
				(old.author_id,old.object_route_id)=(new.author_id,new.object_route_id) then
				new.trend_weight:=old.trend_weight;
			elsif content_target_user_is_developer(new.object_route_id,new.author_id) then new.trend_weight:=0;
			else new.trend_weight:=2*coalesce(content_user_trust(new.author_id),0.2); end if;
			new.counted:=new.status='published' and content_popularity_actor_eligible(new.object_route_id,new.author_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_content_ratings_classify before insert or update on content_ratings
			for each row execute function classify_rating()`,
		`create function reclassify_user_popularity(actor_id bigint) returns void as $$
		begin
			perform 1 from users where id=actor_id for no key update;
			-- A concurrent edit or a change of the other eligibility scope can
			-- already own a vote. Re-read policy after acquiring the vote rows.
			perform 1 from content_ratings where author_id=actor_id order by id for update;
			update content_ratings set counted=not counted
			where author_id=actor_id and counted is distinct from
				(status='published' and content_popularity_actor_eligible(object_route_id,author_id));
			perform reclassify_social_contributions(actor_id,null);
		end;
		$$ language plpgsql`,
		`create function reclassify_project_popularity(project_kind text,project_id bigint) returns void as $$
		declare route_id bigint;
		begin
			select id into route_id from public_routes
			where entity_type=project_kind and internal_id=project_id for no key update;
			if not found then return; end if;
			perform 1 from content_ratings where object_route_id=route_id order by id for update;
			update content_ratings set counted=not counted
			where object_route_id=route_id and counted is distinct from
				(status='published' and content_popularity_actor_eligible(object_route_id,author_id));
			perform reclassify_social_contributions(null,route_id);
		end;
		$$ language plpgsql`,
		`create function reclassify_popularity_from_account() returns trigger as $$
		begin
			perform reclassify_user_popularity(new.id);
			return null;
		end;
		$$ language plpgsql`,
		`create trigger trg_users_rating_eligibility after update of status,security_score on users
			for each row when ((old.status='active' and old.security_score>=40) is distinct from
				(new.status='active' and new.security_score>=40))
			execute function reclassify_popularity_from_account()`,
		`create function reclassify_popularity_from_claim() returns trigger as $$
		begin
			if tg_op<>'INSERT' and old.status='approved' then perform reclassify_user_popularity(old.user_id); end if;
			if tg_op<>'DELETE' and new.status='approved' and (tg_op='INSERT' or
				old.status<>'approved' or old.user_id<>new.user_id) then perform reclassify_user_popularity(new.user_id); end if;
			return null;
		end;
		$$ language plpgsql`,
		`create trigger trg_claims_rating_eligibility
			after insert or update of creator_id,user_id,status or delete on creator_claims
			for each row execute function reclassify_popularity_from_claim()`,
		`create function reclassify_popularity_from_binding() returns trigger as $$
		begin
			if tg_op<>'INSERT' then perform reclassify_project_popularity(old.subject_type,old.subject_id); end if;
			if tg_op='INSERT' or (tg_op='UPDATE' and (old.subject_type,old.subject_id) is distinct from
				(new.subject_type,new.subject_id)) then perform reclassify_project_popularity(new.subject_type,new.subject_id); end if;
			return null;
		end;
		$$ language plpgsql`,
		`create trigger trg_creator_bindings_rating_eligibility
			after insert or update of subject_type,subject_id,creator_id,status,permission_granting or delete on content_creator_bindings
			for each row execute function reclassify_popularity_from_binding()`,
		`create function reclassify_popularity_from_creator() returns trigger as $$
		declare creator_ids bigint[]; actor_id bigint;
		begin
			if tg_table_name='creators' then
				creator_ids:=array[new.id];
				if old.kind='team' or new.kind='team' then
					creator_ids:=creator_ids||array(select member_creator_id from creator_team_members where team_id=new.id);
				end if;
			elsif tg_op='DELETE' then creator_ids:=array[old.member_creator_id];
			elsif tg_op='INSERT' then creator_ids:=array[new.member_creator_id];
			else creator_ids:=array[old.member_creator_id,new.member_creator_id]; end if;
			for actor_id in select distinct claim.user_id from creator_claims claim
				where claim.creator_id=any(creator_ids) and claim.status='approved' order by claim.user_id loop
				perform reclassify_user_popularity(actor_id);
			end loop;
			return null;
		end;
		$$ language plpgsql`,
		`create trigger trg_creators_rating_eligibility after update of kind,review_status on creators
			for each row when ((old.kind,old.review_status) is distinct from (new.kind,new.review_status))
			execute function reclassify_popularity_from_creator()`,
		`create trigger trg_team_members_rating_eligibility
			after insert or update of team_id,member_creator_id,status or delete on creator_team_members
			for each row execute function reclassify_popularity_from_creator()`,
		`create function reclassify_popularity_from_publication() returns trigger as $$
		begin
			if tg_table_name='simple_projects' then perform reclassify_project_popularity(new.project_type,new.id);
			else perform reclassify_project_popularity(tg_argv[0],new.id); end if;
			return null;
		end;
		$$ language plpgsql`,
		`create trigger trg_mods_rating_eligibility after update of review_status on mods
			for each row when ((old.review_status='approved') <> (new.review_status='approved'))
			execute function reclassify_popularity_from_publication('mod')`,
		`create trigger trg_modpacks_rating_eligibility after update of review_status on modpacks
			for each row when ((old.review_status='approved') <> (new.review_status='approved'))
			execute function reclassify_popularity_from_publication('modpack')`,
		`create trigger trg_simple_projects_rating_eligibility after update of review_status on simple_projects
			for each row when ((old.review_status='approved') <> (new.review_status='approved'))
			execute function reclassify_popularity_from_publication()`,
		`create trigger trg_servers_rating_eligibility after update of review_status on minecraft_servers
			for each row when ((old.review_status='approved') <> (new.review_status='approved'))
			execute function reclassify_popularity_from_publication('minecraft_server')`,
	}
}
