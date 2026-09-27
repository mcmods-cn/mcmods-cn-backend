package database

// adminDashboardSchemaStatements installs compact projections for the
// administration workbench. Historical chart reads never aggregate the raw
// activity stream, and all project families share one authoritative view.
func adminDashboardSchemaStatements() []string {
	return []string{
		`create view top_level_project_catalog as
		 select route.id object_route_id,route.public_id,route.entity_type,route.canonical_path,
			project.primary_name name,project.review_status,project.submitted_by,project.created_at,project.updated_at
		 from mods project join public_routes route on route.entity_type='mod' and route.internal_id=project.id
		 union all
		 select route.id,route.public_id,route.entity_type,route.canonical_path,
			project.primary_name,project.review_status,project.submitted_by,project.created_at,project.updated_at
		 from modpacks project join public_routes route on route.entity_type='modpack' and route.internal_id=project.id
		 union all
		 select route.id,route.public_id,route.entity_type,route.canonical_path,
			project.primary_name,project.review_status,project.submitted_by,project.created_at,project.updated_at
		 from simple_projects project join public_routes route
		 	on route.entity_type=project.project_type and route.internal_id=project.id
		 union all
		 select route.id,route.public_id,route.entity_type,route.canonical_path,
			project.name,project.review_status,project.submitted_by,project.created_at,project.updated_at
		 from minecraft_servers project join public_routes route
		 	on route.entity_type='minecraft_server' and route.internal_id=project.id`,
		`create table admin_project_catalog (
			object_route_id bigint primary key references public_routes(id) on delete cascade,
			public_id text not null unique,
			entity_type text not null,
			name text not null,
			canonical_path text not null,
			review_status text not null,
			view_count bigint not null default 0 check(view_count>=0),
			edit_count bigint not null default 0 check(edit_count>=0),
			heat_score numeric(16,6) not null default 0 check(heat_score>=0),
			rating_average numeric(6,4) not null default 0 check(rating_average between 0 and 5),
			favorite_count bigint not null default 0 check(favorite_count>=0),
			comment_count bigint not null default 0 check(comment_count>=0),
			download_count bigint not null default 0 check(download_count>=0),
			created_at timestamptz not null,
			updated_at timestamptz not null,
			last_edited_at timestamptz,
			search_document tsvector generated always as (
				setweight(to_tsvector('simple',public_id),'A') ||
				setweight(to_tsvector('simple',name),'B')
			) stored
		)`,
		`create index idx_admin_project_catalog_heat
			on admin_project_catalog(heat_score desc,updated_at desc,object_route_id desc)`,
		`create index idx_admin_project_catalog_type_heat
			on admin_project_catalog(entity_type,heat_score desc,updated_at desc,object_route_id desc)`,
		`create index idx_admin_project_catalog_search
			on admin_project_catalog using gin(search_document)`,
		`create or replace function refresh_admin_project_catalog(target_route_id bigint) returns void as $$
		begin
			insert into admin_project_catalog(
				object_route_id,public_id,entity_type,name,canonical_path,review_status,
				view_count,edit_count,heat_score,rating_average,favorite_count,comment_count,
				download_count,created_at,updated_at,last_edited_at
			)
			select project.object_route_id,project.public_id,project.entity_type,project.name,
				project.canonical_path,project.review_status,coalesce(metrics.total_view_count,0),
				coalesce(metrics.edit_count,0),coalesce(popularity.heat_score,0),
				coalesce(popularity.rating_average,0),coalesce(popularity.favorite_count,0),
				coalesce(popularity.comment_count,0),coalesce(popularity.download_count,0),
				project.created_at,project.updated_at,metrics.last_edited_at
			from top_level_project_catalog project
			left join content_route_metrics metrics on metrics.object_route_id=project.object_route_id
			left join content_popularity_stats popularity on popularity.object_route_id=project.object_route_id
			where project.object_route_id=target_route_id
			on conflict(object_route_id) do update set
				public_id=excluded.public_id,entity_type=excluded.entity_type,name=excluded.name,
				canonical_path=excluded.canonical_path,review_status=excluded.review_status,
				view_count=excluded.view_count,edit_count=excluded.edit_count,
				heat_score=excluded.heat_score,rating_average=excluded.rating_average,
				favorite_count=excluded.favorite_count,comment_count=excluded.comment_count,
				download_count=excluded.download_count,created_at=excluded.created_at,
				updated_at=excluded.updated_at,last_edited_at=excluded.last_edited_at;
			if not found then
				delete from admin_project_catalog where object_route_id=target_route_id;
			end if;
		end;
		$$ language plpgsql`,
		`create or replace function refresh_admin_project_catalog_from_route() returns trigger as $$
		begin
			perform refresh_admin_project_catalog(new.id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_admin_project_catalog_routes
			after insert or update of public_id,entity_type,internal_id,canonical_path on public_routes
			for each row execute function refresh_admin_project_catalog_from_route()`,
		`create or replace function refresh_admin_project_catalog_from_project() returns trigger as $$
		declare target_type text; target_route_id bigint;
		begin
			target_type:=tg_argv[0];
			select id into target_route_id from public_routes
			where entity_type=target_type and internal_id=new.id;
			perform refresh_admin_project_catalog(target_route_id);
			return new;
		end;
		$$ language plpgsql`,
		`create or replace function refresh_admin_project_catalog_from_simple_project() returns trigger as $$
		declare target_route_id bigint;
		begin
			select id into target_route_id from public_routes
			where entity_type=new.project_type and internal_id=new.id;
			perform refresh_admin_project_catalog(target_route_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_admin_project_catalog_mods after update on mods
			for each row execute function refresh_admin_project_catalog_from_project('mod')`,
		`create trigger trg_admin_project_catalog_modpacks after update on modpacks
			for each row execute function refresh_admin_project_catalog_from_project('modpack')`,
		`create trigger trg_admin_project_catalog_simple_projects after update on simple_projects
			for each row execute function refresh_admin_project_catalog_from_simple_project()`,
		`create trigger trg_admin_project_catalog_servers after update on minecraft_servers
			for each row execute function refresh_admin_project_catalog_from_project('minecraft_server')`,
		`create or replace function refresh_admin_project_catalog_from_stats() returns trigger as $$
		begin
			perform refresh_admin_project_catalog(new.object_route_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_admin_project_catalog_metrics after insert or update on content_route_metrics
			for each row execute function refresh_admin_project_catalog_from_stats()`,
		`create trigger trg_admin_project_catalog_popularity after insert or update on content_popularity_stats
			for each row execute function refresh_admin_project_catalog_from_stats()`,
		`select refresh_admin_project_catalog(object_route_id) from top_level_project_catalog`,
		`create table site_daily_metrics (
			metric_date date primary key,
			active_users bigint not null default 0 check(active_users>=0),
			views bigint not null default 0 check(views>=0),
			actions bigint not null default 0 check(actions>=0),
			new_users bigint not null default 0 check(new_users>=0),
			review_submissions bigint not null default 0 check(review_submissions>=0),
			updated_at timestamptz not null default now()
		)`,
		`create table site_view_daily (
			metric_date date not null default current_date,
			counter_shard smallint not null check(counter_shard between 0 and 31),
			views bigint not null default 0 check(views>=0),
			primary key(metric_date,counter_shard)
		)`,
		`create table site_monthly_active_users (
			activity_month date not null,
			user_id bigint not null references users(id) on delete cascade,
			last_active_date date not null,
			primary key(activity_month,user_id)
		)`,
		`create index idx_site_monthly_active_users_recent
			on site_monthly_active_users(activity_month,last_active_date desc,user_id)`,
		`create table site_daily_active_users (
			activity_date date not null,
			user_id bigint not null references users(id) on delete cascade,
			first_seen_at timestamptz not null,
			primary key(activity_date,user_id)
		)`,
		`create index idx_users_created_at on users(created_at,id)`,
		`create index idx_change_requests_submitted_at on change_requests(submitted_at,id)`,
		`create or replace function record_site_activity_batch(event_times timestamptz[],actor_ids bigint[]) returns void as $$
		begin
			with input as (
				select (value.event_time at time zone 'UTC')::date activity_date,value.user_id,value.event_time
				from unnest(event_times,actor_ids) value(event_time,user_id)
				where value.user_id is not null
			), inserted_daily as (
				insert into site_daily_active_users(activity_date,user_id,first_seen_at)
				select activity_date,user_id,min(event_time) from input group by activity_date,user_id
				on conflict(activity_date,user_id) do nothing
				returning activity_date,true inserted
			), monthly as (
				insert into site_monthly_active_users(activity_month,user_id,last_active_date)
				select date_trunc('month',activity_date)::date,user_id,max(activity_date)
				from input group by date_trunc('month',activity_date)::date,user_id
				on conflict(activity_month,user_id) do update
				set last_active_date=greatest(site_monthly_active_users.last_active_date,excluded.last_active_date)
			), daily_counts as (
				select input.activity_date,count(*) action_count,
					coalesce((select count(*) from inserted_daily inserted
						where inserted.activity_date=input.activity_date and inserted.inserted),0) active_count
				from input group by input.activity_date
			)
			insert into site_daily_metrics(metric_date,active_users,actions,updated_at)
			select activity_date,active_count,action_count,now() from daily_counts
			on conflict(metric_date) do update set
				active_users=site_daily_metrics.active_users+excluded.active_users,
				actions=site_daily_metrics.actions+excluded.actions,updated_at=now();
		end;
		$$ language plpgsql`,
		`create or replace function refresh_site_daily_metrics(target_date date) returns void as $$
		declare
			day_start timestamptz := target_date::timestamp at time zone 'UTC';
			day_end timestamptz := (target_date+1)::timestamp at time zone 'UTC';
			active_count bigint; view_count bigint; action_count bigint;
			new_user_count bigint; review_count bigint;
		begin
			select count(distinct user_id),count(*) into active_count,action_count
			from user_activity_events where user_id is not null and occurred_at>=day_start and occurred_at<day_end;
			select coalesce(sum(views),0) into view_count from site_view_daily where metric_date=target_date;
			select count(*) into new_user_count from users where created_at>=day_start and created_at<day_end;
			select count(*) into review_count from change_requests where submitted_at>=day_start and submitted_at<day_end;
			insert into site_monthly_active_users(activity_month,user_id,last_active_date)
			select date_trunc('month',target_date)::date,user_id,target_date
			from user_activity_events where user_id is not null and occurred_at>=day_start and occurred_at<day_end
			group by user_id
			on conflict(activity_month,user_id) do update
			set last_active_date=greatest(site_monthly_active_users.last_active_date,excluded.last_active_date);
			insert into site_daily_active_users(activity_date,user_id,first_seen_at)
			select target_date,user_id,min(occurred_at) from user_activity_events
			where user_id is not null and occurred_at>=day_start and occurred_at<day_end group by user_id
			on conflict(activity_date,user_id) do nothing;
			insert into site_daily_metrics(metric_date,active_users,views,actions,new_users,review_submissions,updated_at)
			values(target_date,active_count,view_count,action_count,new_user_count,review_count,now())
			on conflict(metric_date) do update set active_users=excluded.active_users,views=excluded.views,
				actions=excluded.actions,new_users=excluded.new_users,review_submissions=excluded.review_submissions,
				updated_at=now();
		end;
		$$ language plpgsql`,
		`create or replace function refresh_site_current_counters() returns void as $$
		begin
			insert into site_daily_metrics(metric_date,new_users,review_submissions,updated_at)
			values(current_date,
				(select count(*) from users where created_at>=current_date),
				(select count(*) from change_requests where submitted_at>=current_date),now())
			on conflict(metric_date) do update set new_users=excluded.new_users,
				review_submissions=excluded.review_submissions,updated_at=now();
		end;
		$$ language plpgsql`,
		`insert into site_view_daily(metric_date,counter_shard,views)
			select view_date,0,sum(views) from content_view_daily group by view_date
			on conflict(metric_date,counter_shard) do update set views=excluded.views`,
		`select refresh_site_daily_metrics(day::date)
			from generate_series(current_date-29,current_date,interval '1 day') day`,
	}
}
