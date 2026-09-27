-- Run with psql only against an isolated test/staging database after loading at
-- least 100k catalog rows, 1M activity rows, and one deliberately high-cardinality
-- user. Example:
-- psql ... -v representative_user_id=42 -v catalog_offset=10000 -v representative_action_id=3 -f scripts/query-analysis.sql
-- The DELETE plan executes inside an explicit transaction that is always rolled back.

\if :{?representative_user_id}
\else
  \echo 'representative_user_id is required'
  \quit
\endif
\if :{?catalog_offset}
\else
  \echo 'catalog_offset is required (use a representative deep page such as 10000)'
  \quit
\endif
\if :{?representative_action_id}
\else
  \echo 'representative_action_id is required'
  \quit
\endif

explain (analyze, buffers, format text)
select project.id
from mods project
left join public_routes route on route.entity_type='mod' and route.internal_id=project.id
left join content_popularity_stats popularity on popularity.object_route_id=route.id
where project.review_status='approved'
order by coalesce(popularity.heat_score,0) desc,project.updated_at desc,project.id desc
limit 20 offset :catalog_offset;

explain (analyze, buffers, format text)
select project.id
from simple_projects project
left join public_routes route on route.entity_type=project.project_type and route.internal_id=project.id
left join content_popularity_stats popularity on popularity.object_route_id=route.id
where project.project_type='plugin' and project.review_status='approved'
  and '1.20.1'=any(project.minecraft_versions)
  and 'utility'=any(project.categories)
order by coalesce(popularity.heat_score,0) desc,project.updated_at desc,project.id desc
limit 20;

explain (analyze, buffers, format text)
select server.id
from minecraft_servers server
left join public_routes route on route.entity_type='minecraft_server' and route.internal_id=server.id
left join content_popularity_stats popularity on popularity.object_route_id=route.id
where server.review_status='approved'
order by coalesce(popularity.heat_score,0) desc,server.updated_at desc,server.id desc
limit 20;

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
  exists(select 1 from user_presence_sessions presence
    where presence.user_id=account.id and presence.last_active_at>=now()-interval '5 minutes'),
  account.public_card_stat_slots,
  coalesce(statistics.edit_count,0),coalesce(statistics.markdown_added_bytes+statistics.markdown_deleted_bytes,0)
from users account
left join user_statistics_totals statistics on statistics.user_id=account.id
where account.id=:'representative_user_id'::bigint;

explain (analyze, buffers, format text)
select user_id,last_active_at
from user_presence_sessions
where user_id=:'representative_user_id'::bigint
  and last_active_at>=now()-interval '5 minutes'
limit 1;

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
select id,user_id,action_id,object_type_id,occurred_at
from activity_event_outbox
where available_at<=now()
order by available_at,id
limit 256
for update skip locked;

explain (analyze, buffers, format text)
select count(*),min(created_at)
from activity_event_outbox;
