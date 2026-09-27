package database

// projectAccessSchemaStatements defines the two relationship-derived ways a
// user can receive project-scoped access: an approved editor assignment, or an
// approved personal-author claim connected through a verified author/team
// relation. Explicit administrator RBAC grants remain independent of this view.
// Revoking one derived source therefore cannot erase another source or a
// deliberate administrator grant.
func projectAccessSchemaStatements() []string {
	return []string{
		`create or replace view published_project_routes as
		 select route.id,route.public_id,route.entity_type,route.internal_id
		 from public_routes route join mods target on route.entity_type='mod' and route.internal_id=target.id
		 where target.review_status='approved'
		 union all
		 select route.id,route.public_id,route.entity_type,route.internal_id
		 from public_routes route join modpacks target on route.entity_type='modpack' and route.internal_id=target.id
		 where target.review_status='approved'
		 union all
		 select route.id,route.public_id,route.entity_type,route.internal_id
		 from public_routes route join simple_projects target on route.entity_type=target.project_type and route.internal_id=target.id
		 where target.review_status='approved'
		 union all
		 select route.id,route.public_id,route.entity_type,route.internal_id
		 from public_routes route join minecraft_servers target on route.entity_type='minecraft_server' and route.internal_id=target.id
		 where target.review_status='approved'
		 union all
		 select route.id,route.public_id,route.entity_type,route.internal_id
		 from public_routes route join blueprints target on route.entity_type='blueprint' and route.internal_id=target.id
		 where target.status in ('ready','partial') and target.review_status in ('not_required','approved')
		 union all
		 select route.id,route.public_id,route.entity_type,route.internal_id
		 from public_routes route join skin_assets target on route.entity_type='skin' and route.internal_id=target.id
		 where target.status='active' and target.visibility='public' and target.review_status='approved'
		 union all
		 select route.id,route.public_id,route.entity_type,route.internal_id
		 from public_routes route join community_posts target on route.entity_type='community_post' and route.internal_id=target.id
		 where target.status='active' and target.review_status='approved'`,
		`create or replace view effective_project_access as
		 select assignment.user_id,route.entity_type project_type,route.internal_id project_id,
			route.public_id project_public_id,'editor'::text access_level,
			'editor_assignment'::text source_type,assignment.target_route_id source_id,
			'editor_assignment:'||assignment.target_route_id::text source_path
		 from project_editor_assignments assignment
		 join published_project_routes route on route.id=assignment.target_route_id
		 where assignment.status='active'
		 union all
		 select claim.user_id,binding.subject_type,binding.subject_id,route.public_id,
			'developer'::text,'author_claim'::text,claim.id,
			'author_claim:'||claim.id::text||'/project_author_relation:'||binding.id::text
		 from creator_claims claim
		 join creators author on author.id=claim.creator_id and author.kind='author' and author.review_status='approved'
		 join content_creator_bindings binding on binding.creator_id=author.id
		 join published_project_routes route on route.entity_type=binding.subject_type and route.internal_id=binding.subject_id
		 where claim.status='approved' and binding.status='approved' and binding.permission_granting
		 union all
		 select claim.user_id,binding.subject_type,binding.subject_id,route.public_id,
			'developer'::text,'author_team_relation'::text,membership.team_id,
			'author_claim:'||claim.id::text||'/team_membership:'||membership.team_id::text||':'||membership.member_creator_id::text||
			'/project_team_relation:'||binding.id::text
		 from creator_claims claim
		 join creators author on author.id=claim.creator_id and author.kind='author' and author.review_status='approved'
		 join creator_team_members membership on membership.member_creator_id=author.id and membership.status='approved'
		 join creators team on team.id=membership.team_id and team.kind='team' and team.review_status='approved'
		 join content_creator_bindings binding on binding.creator_id=team.id
		 join published_project_routes route on route.entity_type=binding.subject_type and route.internal_id=binding.subject_id
		 where claim.status='approved' and binding.status='approved' and binding.permission_granting`,
		`create index idx_project_editor_assignments_user_active
			on project_editor_assignments(user_id,target_route_id) where status='active'`,
		`create index idx_creator_claims_user_approved
			on creator_claims(user_id,creator_id) where status='approved'`,
		`create index idx_creator_team_members_approved_member
			on creator_team_members(member_creator_id,team_id) where status='approved'`,
		`create index idx_content_creator_bindings_access
			on content_creator_bindings(creator_id,subject_type,subject_id)
			where status='approved' and permission_granting`,
		`create or replace function bump_project_access_user_permission_version() returns trigger as $$
		begin
			if tg_table_name='creator_claims' then
				if tg_op='INSERT' and new.status<>'approved' then return new; end if;
				if tg_op='DELETE' and old.status<>'approved' then return old; end if;
				if tg_op='UPDATE' and old.status<>'approved' and new.status<>'approved' then return new; end if;
			end if;
			if tg_table_name='project_editor_assignments' then
				if tg_op='INSERT' and new.status<>'active' then return new; end if;
				if tg_op='DELETE' and old.status<>'active' then return old; end if;
				if tg_op='UPDATE' and old.status<>'active' and new.status<>'active' then return new; end if;
			end if;
			if tg_op='DELETE' then
				update users set permission_version=permission_version+1,updated_at=now() where id=old.user_id;
				return old;
			elsif tg_op='INSERT' then
				update users set permission_version=permission_version+1,updated_at=now() where id=new.user_id;
				return new;
			end if;
			update users set permission_version=permission_version+1,updated_at=now()
			where id in (old.user_id,new.user_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_creator_claims_permission_version
			after insert or update or delete on creator_claims
			for each row execute function bump_project_access_user_permission_version()`,
		`create trigger trg_project_editor_assignments_permission_version
			after insert or update or delete on project_editor_assignments
			for each row execute function bump_project_access_user_permission_version()`,
		`create or replace function bump_project_acl_runtime_version() returns trigger as $$
		begin
			if current_setting('mcmods.project_acl_runtime_bumped',true)='1' then return null; end if;
			perform set_config('mcmods.project_acl_runtime_bumped','1',true);
			perform bump_runtime_version('project_acl');
			return null;
		end;
		$$ language plpgsql`,
		`create trigger trg_content_creator_bindings_project_acl
			after insert or update or delete on content_creator_bindings
			for each statement execute function bump_project_acl_runtime_version()`,
		`create trigger trg_creator_team_members_project_acl
			after insert or update or delete on creator_team_members
			for each statement execute function bump_project_acl_runtime_version()`,
		`create trigger trg_creator_role_definitions_project_acl
			after update of permission_granting on creator_role_definitions
			for each statement execute function bump_project_acl_runtime_version()`,
	}
}
