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
