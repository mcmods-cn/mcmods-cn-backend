-- See docs/load-testing.md for the isolated dataset and required psql arguments.
-- The activity-retention DELETE is executed in a transaction and rolled back.
\set ON_ERROR_STOP on

\if :{?expected_database}
\else
  do $$begin raise exception 'expected_database is required';end$$;
\endif

\if :{?representative_user_id}
\else
  do $$begin raise exception 'representative_user_id is required';end$$;
\endif
\if :{?catalog_offset}
\else
  do $$begin raise exception 'catalog_offset is required';end$$;
\endif
\if :{?representative_action_id}
\else
  do $$begin raise exception 'representative_action_id is required';end$$;
\endif

select current_database()=:'expected_database' as expected_database_matches \gset
\if :expected_database_matches
\else
  do $$begin raise exception 'connected database does not match expected_database';end$$;
\endif
set search_path to pg_catalog, public;
set time zone 'UTC';

select :'catalog_offset'::integer between 0 and 1000000
  and exists(select 1 from users where id=:'representative_user_id'::bigint)
  and exists(select 1 from activity_actions where id=:'representative_action_id'::smallint)
  as plan_arguments_valid \gset
\if :plan_arguments_valid
\else
  do $$begin raise exception 'choose an existing internal user and action, and a catalog offset from 0 to 1000000';end$$;
\endif

-- SQL ordering and pagination for anonymous Mod heat sorting. The API also
-- reads the total, the card fields, and its associations.
explain (analyze, buffers, format text)
select project.id
from mods project
left join public_routes route on route.entity_type='mod' and route.internal_id=project.id
left join content_popularity_stats popularity on popularity.object_route_id=route.id
where project.review_status='approved'
order by coalesce(popularity.heat_score,0) desc,project.updated_at desc,project.id desc
limit 20 offset :'catalog_offset'::integer;

-- Public Plugin heat sorting filtered by one Minecraft version and category.
explain (analyze, buffers, format text)
select project.id
from simple_projects project
left join public_routes route on route.entity_type=project.project_type and route.internal_id=project.id
left join content_popularity_stats popularity on popularity.object_route_id=route.id
where project.project_type='plugin' and project.review_status='approved'
  and project.minecraft_versions && array['1.20.1']::text[]
  and 'utility'=any(project.categories)
order by coalesce(popularity.heat_score,0) desc,project.updated_at desc,project.id desc
limit 20;

-- PostgreSQL server catalog first page, with the production online tie-breaker
-- and the additional row used to signal a next cursor.
explain (analyze, buffers, format text)
select server.id
from minecraft_servers server
left join public_routes route on route.entity_type='minecraft_server' and route.internal_id=server.id
left join content_popularity_stats popularity on popularity.object_route_id=route.id
where server.review_status='approved'
order by coalesce((popularity.heat_score*1000000)::bigint,0)*2
           + case when server.last_online then 1 else 0 end desc,
         server.updated_at desc,server.id desc
limit 21;

explain (analyze, buffers, format text)
select stat_date,action_count,edit_count,markdown_added_bytes,markdown_deleted_bytes
from user_statistics_daily
where user_id=:'representative_user_id'::bigint and stat_date>=current_date-89
order by stat_date;

explain (analyze, buffers, format text)
select content_type,count(*),count(*) filter(where current_exists)
from user_content_creation_facts
where user_id=:'representative_user_id'::bigint
group by content_type;

explain (analyze, buffers, format text)
select account.username,account.show_online_status,
  account.public_card_stat_slots,
  coalesce(statistics.edit_count,0),coalesce(statistics.markdown_added_bytes+statistics.markdown_deleted_bytes,0)
from users account
left join user_statistics_totals statistics on statistics.user_id=account.id
where account.id=:'representative_user_id'::bigint;

-- Candidate lookup for pruning persisted authenticated-session snapshots.
-- Live online status is resolved by the presence cache.
explain (analyze, buffers, format text)
select session_hash
from user_presence_sessions
where last_active_at<now()-interval '1 day'
order by last_active_at
limit 1000;

explain (analyze, buffers, format text)
select action_id,count(*)
from user_activity_events
where action_id=:'representative_action_id'::bigint and occurred_at>=now()-interval '30 days' and occurred_at<now()
group by action_id;

explain (analyze, buffers, format text)
select id,action_id,object_type_id,object_route_id,occurred_at
from user_activity_events
where user_id=:'representative_user_id'::bigint
order by occurred_at desc,id desc
limit 50;

explain (analyze, buffers, format text)
select count(*)
from user_activity_events
where action_id=:'representative_action_id'::bigint
  and user_id=:'representative_user_id'::bigint
  and object_type_id=2
  and occurred_at>=now()-interval '30 days' and occurred_at<now();

begin;
explain (analyze, buffers, format text)
delete from user_activity_events
where id in (
  select id from user_activity_events
  where action_id=:'representative_action_id'::bigint and occurred_at<now()-interval '30 days'
  order by occurred_at,id limit 1000
);
rollback;

explain (analyze, buffers, format text)
select id,user_id,action_id,object_type_id,object_route_id,
  object_public_id,object_entity_type,object_internal_id,markdown_added_bytes,
  markdown_deleted_bytes,occurred_at
from activity_event_outbox
where available_at<=now()
order by available_at,id
limit 256
for update skip locked;

explain (analyze, buffers, format text)
select count(*),min(created_at)
from activity_event_outbox;
