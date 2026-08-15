package database

const syncUserContentCreationFactFunctionSQL = `create or replace function sync_user_content_creation_fact() returns trigger as $$
		declare row_data jsonb; actor_id bigint; content_kind text; object_identifier text; review_state text; exists_now boolean; created_time timestamptz;
		begin
			row_data:=case when tg_op='DELETE' then to_jsonb(old) else to_jsonb(new) end;
			actor_id:=coalesce(nullif(row_data->>'created_by','')::bigint,nullif(row_data->>'author_id','')::bigint);
			if actor_id is null then
				if tg_op='DELETE' then return old; end if;
				return new;
			end if;
			content_kind:=coalesce(nullif(tg_argv[0],''),nullif(row_data->>'project_type',''),nullif(row_data->>'kind',''),'other');
			object_identifier:=coalesce(nullif(row_data->>'project_code',''),nullif(row_data->>'public_id',''));
			review_state:=coalesce(nullif(row_data->>'review_status',''),'pending');
			exists_now:=tg_op<>'DELETE' and coalesce(row_data->>'status','active')<>'deleted';
			created_time:=coalesce(nullif(row_data->>'created_at','')::timestamptz,now());
			insert into user_content_creation_facts(content_type,object_key,user_id,review_status,current_exists,created_at,deleted_at)
			values(content_kind,object_identifier,actor_id,review_state,exists_now,created_time,case when exists_now then null else now() end)
			on conflict(content_type,object_key) do update set review_status=excluded.review_status,
				current_exists=excluded.current_exists,deleted_at=excluded.deleted_at,updated_at=now();
			if tg_op='DELETE' then return old; end if;
			return new;
		end $$ language plpgsql`

func userFeatureSchemaStatements() []string {
	return []string{
		`alter table users add column if not exists show_online_status boolean not null default false`,
		`alter table users add column if not exists public_card_stat_slots text[] not null
			default array['','','','','','']::text[] check(cardinality(public_card_stat_slots)=6)`,
		`create table if not exists user_presence_sessions (
			session_hash bytea primary key references auth_sessions(session_hash) on delete cascade,
			user_id bigint not null references users(id) on delete cascade,
			last_active_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`create index if not exists idx_user_presence_sessions_user_active
			on user_presence_sessions(user_id,last_active_at desc)`,
		`alter table user_activity_events add column if not exists markdown_deleted_bytes integer not null default 0
			check(markdown_deleted_bytes>=0)`,
		`create index if not exists idx_activity_action_time
			on user_activity_events(action_id,occurred_at,id)`,
		`create table if not exists activity_cleanup_runs (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			source text not null check(source in ('automatic','manual')),
			initiated_by bigint references users(id) on delete set null,
			status text not null check(status in ('preview','running','completed','failed')),
			filters jsonb not null default '{}'::jsonb,
			confirmation_hash text not null default '',
			matched_count bigint not null default 0 check(matched_count>=0),
			deleted_count bigint not null default 0 check(deleted_count>=0),
			started_at timestamptz not null default now(),
			finished_at timestamptz,
			expires_at timestamptz,
			error_message text not null default ''
		)`,
		`create index if not exists idx_activity_cleanup_runs_schedule
			on activity_cleanup_runs(source,started_at desc,id desc)`,
		`create table if not exists user_statistics_daily (
			user_id bigint not null references users(id) on delete cascade,
			stat_date date not null,
			action_count bigint not null default 0 check(action_count>=0),
			view_count bigint not null default 0 check(view_count>=0),
			edit_count bigint not null default 0 check(edit_count>=0),
			create_count bigint not null default 0 check(create_count>=0),
			delete_count bigint not null default 0 check(delete_count>=0),
			markdown_added_bytes bigint not null default 0 check(markdown_added_bytes>=0),
			markdown_deleted_bytes bigint not null default 0 check(markdown_deleted_bytes>=0),
			action_counts jsonb not null default '{}'::jsonb,
			first_activity_at timestamptz,
			last_activity_at timestamptz,
			updated_at timestamptz not null default now(),
			primary key(user_id,stat_date)
		)`,
		`create index if not exists idx_user_statistics_daily_range
			on user_statistics_daily(user_id,stat_date desc)`,
		`create table if not exists user_statistics_totals (
			user_id bigint primary key references users(id) on delete cascade,
			action_count bigint not null default 0 check(action_count>=0),
			view_count bigint not null default 0 check(view_count>=0),
			edit_count bigint not null default 0 check(edit_count>=0),
			create_count bigint not null default 0 check(create_count>=0),
			delete_count bigint not null default 0 check(delete_count>=0),
			markdown_added_bytes bigint not null default 0 check(markdown_added_bytes>=0),
			markdown_deleted_bytes bigint not null default 0 check(markdown_deleted_bytes>=0),
			action_counts jsonb not null default '{}'::jsonb,
			first_activity_at timestamptz,
			last_activity_at timestamptz,
			last_edit_at timestamptz,
			last_comment_at timestamptz,
			updated_at timestamptz not null default now()
		)`,
		`create table if not exists user_content_creation_facts (
			content_type text not null,
			object_key text not null,
			user_id bigint not null references users(id) on delete cascade,
			review_status text not null default 'pending',
			current_exists boolean not null default true,
			created_at timestamptz not null,
			deleted_at timestamptz,
			updated_at timestamptz not null default now(),
			primary key(content_type,object_key),
			check(review_status in ('pending','approved','rejected'))
		)`,
		`create index if not exists idx_user_content_creation_facts_user
			on user_content_creation_facts(user_id,created_at desc,content_type)`,
		syncUserContentCreationFactFunctionSQL,
		`drop trigger if exists trg_mods_user_creation_fact on mods`,
		`create trigger trg_mods_user_creation_fact after insert or update of review_status or delete on mods
			for each row execute function sync_user_content_creation_fact('mod')`,
		`drop trigger if exists trg_modpacks_user_creation_fact on modpacks`,
		`create trigger trg_modpacks_user_creation_fact after insert or update of review_status or delete on modpacks
			for each row execute function sync_user_content_creation_fact('modpack')`,
		`drop trigger if exists trg_simple_projects_user_creation_fact on simple_projects`,
		`create trigger trg_simple_projects_user_creation_fact after insert or update of review_status or delete on simple_projects
			for each row execute function sync_user_content_creation_fact('')`,
		`drop trigger if exists trg_servers_user_creation_fact on minecraft_servers`,
		`create trigger trg_servers_user_creation_fact after insert or update of review_status or delete on minecraft_servers
			for each row execute function sync_user_content_creation_fact('server')`,
		`drop trigger if exists trg_community_posts_user_creation_fact on community_posts`,
		`create trigger trg_community_posts_user_creation_fact after insert or update of review_status,status or delete on community_posts
			for each row execute function sync_user_content_creation_fact('')`,
		`create or replace function increment_jsonb_counter(counters jsonb,counter_key text,amount bigint) returns jsonb as $$
			select jsonb_set(coalesce(counters,'{}'::jsonb),array[counter_key],
				to_jsonb(coalesce((counters->>counter_key)::bigint,0)+amount),true)
		$$ language sql immutable`,
		`create or replace function accumulate_user_statistics() returns trigger as $$
		declare action_code text;
		begin
			if new.user_id is null then return new; end if;
			select code into action_code from activity_actions where id=new.action_id;
			insert into user_statistics_daily(user_id,stat_date,action_count,view_count,edit_count,create_count,delete_count,
				markdown_added_bytes,markdown_deleted_bytes,action_counts,first_activity_at,last_activity_at)
			values(new.user_id,(new.occurred_at at time zone 'UTC')::date,1,
				case when action_code='view' then 1 else 0 end,case when action_code='edit' then 1 else 0 end,
				case when action_code='create' then 1 else 0 end,case when action_code='delete' then 1 else 0 end,
				new.markdown_added_bytes,new.markdown_deleted_bytes,jsonb_build_object(action_code,1),new.occurred_at,new.occurred_at)
			on conflict(user_id,stat_date) do update set
				action_count=user_statistics_daily.action_count+1,
				view_count=user_statistics_daily.view_count+excluded.view_count,
				edit_count=user_statistics_daily.edit_count+excluded.edit_count,
				create_count=user_statistics_daily.create_count+excluded.create_count,
				delete_count=user_statistics_daily.delete_count+excluded.delete_count,
				markdown_added_bytes=user_statistics_daily.markdown_added_bytes+excluded.markdown_added_bytes,
				markdown_deleted_bytes=user_statistics_daily.markdown_deleted_bytes+excluded.markdown_deleted_bytes,
				action_counts=increment_jsonb_counter(user_statistics_daily.action_counts,action_code,1),
				first_activity_at=least(user_statistics_daily.first_activity_at,excluded.first_activity_at),
				last_activity_at=greatest(user_statistics_daily.last_activity_at,excluded.last_activity_at),updated_at=now();
			insert into user_statistics_totals(user_id,action_count,view_count,edit_count,create_count,delete_count,
				markdown_added_bytes,markdown_deleted_bytes,action_counts,first_activity_at,last_activity_at,last_edit_at,last_comment_at)
			values(new.user_id,1,case when action_code='view' then 1 else 0 end,case when action_code='edit' then 1 else 0 end,
				case when action_code='create' then 1 else 0 end,case when action_code='delete' then 1 else 0 end,
				new.markdown_added_bytes,new.markdown_deleted_bytes,jsonb_build_object(action_code,1),new.occurred_at,new.occurred_at,
				case when action_code='edit' then new.occurred_at end,
				case when action_code='create' and new.object_type_id=8 then new.occurred_at end)
			on conflict(user_id) do update set
				action_count=user_statistics_totals.action_count+1,
				view_count=user_statistics_totals.view_count+excluded.view_count,
				edit_count=user_statistics_totals.edit_count+excluded.edit_count,
				create_count=user_statistics_totals.create_count+excluded.create_count,
				delete_count=user_statistics_totals.delete_count+excluded.delete_count,
				markdown_added_bytes=user_statistics_totals.markdown_added_bytes+excluded.markdown_added_bytes,
				markdown_deleted_bytes=user_statistics_totals.markdown_deleted_bytes+excluded.markdown_deleted_bytes,
				action_counts=increment_jsonb_counter(user_statistics_totals.action_counts,action_code,1),
				first_activity_at=least(user_statistics_totals.first_activity_at,excluded.first_activity_at),
				last_activity_at=greatest(user_statistics_totals.last_activity_at,excluded.last_activity_at),
				last_edit_at=greatest(user_statistics_totals.last_edit_at,excluded.last_edit_at),
				last_comment_at=greatest(user_statistics_totals.last_comment_at,excluded.last_comment_at),updated_at=now();
			return new;
		end $$ language plpgsql`,
		`drop trigger if exists trg_user_activity_statistics on user_activity_events`,
		`create trigger trg_user_activity_statistics after insert on user_activity_events
			for each row execute function accumulate_user_statistics()`,
	}
}
