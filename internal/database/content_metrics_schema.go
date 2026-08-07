package database

// contentMetricsSchemaStatements installs the shared, persisted statistics
// projection used by both small catalog resources and top-level projects.
// Writes only mark a numeric route dirty; the background worker coalesces
// repeated views and edits before rebuilding the projection.
func contentMetricsSchemaStatements() []string {
	return []string{
		`create table content_route_metrics (
			object_route_id bigint primary key references public_routes(id) on delete cascade,
			direct_view_count bigint not null default 0 check(direct_view_count>=0),
			child_view_count bigint not null default 0 check(child_view_count>=0),
			total_view_count bigint not null default 0 check(total_view_count>=0),
			edit_count bigint not null default 0 check(edit_count>=0),
			created_at timestamptz not null,
			last_edited_at timestamptz,
			updated_at timestamptz not null default now()
		)`,
		`create index idx_content_route_metrics_views
			on content_route_metrics(total_view_count desc,object_route_id)`,
		`create table content_stats_refresh_queue (
			object_route_id bigint primary key references public_routes(id) on delete cascade,
			refresh_metrics boolean not null default true,
			refresh_popularity boolean not null default false,
			attempts integer not null default 0 check(attempts>=0),
			available_at timestamptz not null default now(),
			locked_at timestamptz,
			last_error text not null default '',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`create index idx_content_stats_refresh_ready
			on content_stats_refresh_queue(available_at,updated_at,object_route_id)`,
		`create or replace function enqueue_content_stats_refresh(
			target_route_id bigint,
			include_metrics boolean default true,
			include_popularity boolean default false
		) returns void as $$
		begin
			if target_route_id is null then return; end if;
			insert into content_stats_refresh_queue(
				object_route_id,refresh_metrics,refresh_popularity,available_at,locked_at,last_error,updated_at
			) values(target_route_id,include_metrics,include_popularity,now(),null,'',now())
			on conflict(object_route_id) do update set
				refresh_metrics=content_stats_refresh_queue.refresh_metrics or excluded.refresh_metrics,
				refresh_popularity=content_stats_refresh_queue.refresh_popularity or excluded.refresh_popularity,
				available_at=least(content_stats_refresh_queue.available_at,excluded.available_at),
				locked_at=null,last_error='',updated_at=now();
		end;
		$$ language plpgsql`,
		`create or replace function enqueue_parent_project_metrics(target_route_id bigint) returns void as $$
		declare target_type text; target_id bigint; parent_route_id bigint;
		begin
			select entity_type,internal_id into target_type,target_id from public_routes where id=target_route_id;
			if target_type='resource' then
				select parent.id into parent_route_id
				from mod_resource_bindings binding
				join public_routes parent on parent.entity_type='mod' and parent.internal_id=binding.mod_id
				where binding.resource_id=target_id;
				perform enqueue_content_stats_refresh(parent_route_id,true,true);
			end if;
		end;
		$$ language plpgsql`,
		`create or replace function refresh_content_route_metrics(target_route_id bigint) returns void as $$
		declare
			target_type text; target_id bigint; route_created_at timestamptz;
			direct_views bigint; child_views bigint; manual_edits bigint; imported_edits bigint;
			last_manual_edit timestamptz; last_import_edit timestamptz;
		begin
			select entity_type,internal_id,created_at into target_type,target_id,route_created_at
			from public_routes where id=target_route_id;
			if not found then return; end if;

			select coalesce(sum(views),0) into direct_views
			from content_view_daily where object_route_id=target_route_id;
			child_views:=0;
			if target_type='mod' then
				select coalesce(sum(daily.views),0) into child_views
				from mod_resource_bindings binding
				join public_routes child on child.entity_type='resource' and child.internal_id=binding.resource_id
				join content_view_daily daily on daily.object_route_id=child.id
				where binding.mod_id=target_id;
			end if;

			select count(*),max(revision.created_at) into manual_edits,last_manual_edit
			from content_revisions revision
			join change_requests request on request.proposed_revision_id=revision.id and request.status='approved'
			where revision.entity_type=target_type and revision.entity_id=target_id;
			imported_edits:=0;
			last_import_edit:=null;
			if target_type='resource' then
				select count(distinct imported.id),max(imported.created_at)
				into imported_edits,last_import_edit
				from resource_import_snapshots snapshot
				join catalog_import_revisions imported on imported.id=snapshot.revision_id
				where snapshot.resource_id=target_id and imported.status in ('ready','partial','superseded');
			end if;

			insert into content_route_metrics(
				object_route_id,direct_view_count,child_view_count,total_view_count,edit_count,
				created_at,last_edited_at,updated_at
			) values(
				target_route_id,direct_views,child_views,direct_views+child_views,manual_edits+imported_edits,
				route_created_at,greatest(last_manual_edit,last_import_edit),now()
			) on conflict(object_route_id) do update set
				direct_view_count=excluded.direct_view_count,
				child_view_count=excluded.child_view_count,
				total_view_count=excluded.total_view_count,
				edit_count=excluded.edit_count,
				created_at=excluded.created_at,
				last_edited_at=excluded.last_edited_at,
				updated_at=now();
		end;
		$$ language plpgsql`,
		`create or replace function enqueue_metrics_from_revision() returns trigger as $$
		declare route_id bigint;
		begin
			if new.entity_type is null or new.entity_id is null then return new; end if;
			select id into route_id from public_routes
			where entity_type=new.entity_type and internal_id=new.entity_id;
			perform enqueue_content_stats_refresh(route_id,true,false);
			perform enqueue_parent_project_metrics(route_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_content_revisions_metrics after insert on content_revisions
			for each row execute function enqueue_metrics_from_revision()`,
		`create or replace function enqueue_metrics_from_review_resolution() returns trigger as $$
		declare route_id bigint;
		begin
			if new.status=old.status or new.entity_type is null or new.entity_id is null then return new; end if;
			select id into route_id from public_routes
			where entity_type=new.entity_type and internal_id=new.entity_id;
			perform enqueue_content_stats_refresh(route_id,true,false);
			perform enqueue_parent_project_metrics(route_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_change_requests_metrics after update of status on change_requests
			for each row execute function enqueue_metrics_from_review_resolution()`,
		`create or replace function enqueue_metrics_from_import_snapshot() returns trigger as $$
		declare route_id bigint;
		begin
			select id into route_id from public_routes where entity_type='resource' and internal_id=new.resource_id;
			perform enqueue_content_stats_refresh(route_id,true,false);
			perform enqueue_parent_project_metrics(route_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_resource_import_snapshots_metrics after insert or update of revision_id
			on resource_import_snapshots for each row execute function enqueue_metrics_from_import_snapshot()`,
		`create or replace function enqueue_metrics_from_import_resolution() returns trigger as $$
		declare route_id bigint;
		begin
			if new.status=old.status then return new; end if;
			for route_id in
				select distinct route.id from resource_import_snapshots snapshot
				join public_routes route on route.entity_type='resource' and route.internal_id=snapshot.resource_id
				where snapshot.revision_id=new.id
			loop
				perform enqueue_content_stats_refresh(route_id,true,false);
				perform enqueue_parent_project_metrics(route_id);
			end loop;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_catalog_import_revisions_metrics after update of status on catalog_import_revisions
			for each row execute function enqueue_metrics_from_import_resolution()`,
		`create index idx_content_unique_views_route_recent_user
			on content_unique_views(object_route_id,last_seen_at desc,viewer_user_id)
			where viewer_user_id is not null`,
		`create index idx_comments_target_version_published_author
			on comments(target_type,target_version_id,author_id)
			where status='published'`,
		`insert into content_stats_refresh_queue(object_route_id,refresh_metrics,refresh_popularity)
		select route.id,true,route.entity_type in
			('mod','modpack','plugin','map','resource_pack','shader_pack','datapack','addon','minecraft_server')
		from public_routes route
		where route.entity_type in
			('resource','mod','modpack','plugin','map','resource_pack','shader_pack','datapack','addon','minecraft_server')
		on conflict(object_route_id) do nothing`,
	}
}
