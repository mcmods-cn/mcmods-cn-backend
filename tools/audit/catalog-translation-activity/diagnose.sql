-- Bounded operator-only candidate counts. No repair or reward replay.
begin transaction isolation level repeatable read read only;
set local statement_timeout='10s';
set local lock_timeout='2s';
set local search_path to pg_catalog;
do $scope$
begin
 if current_database() is distinct from current_setting('mcmods.audit_database',true) then
  raise exception 'AUDIT_CATALOG_ACTIVITY_DATABASE_MISMATCH' using errcode='22023';
 end if;
 if coalesce(current_setting('mcmods.audit_schema',true),'') !~ '^[a-z_][a-z0-9_]{0,62}$'
  or coalesce(current_setting('mcmods.audit_actor',true),'') !~ '^[a-z0-9]{9}$'
  or coalesce(current_setting('mcmods.audit_target',true),'') !~ '^[a-z0-9]{9}$'
  or coalesce(current_setting('mcmods.audit_object_code',true),'') not in ('mod','resource','tag','blueprint','recipe')
  or coalesce(current_setting('mcmods.audit_since',true),'')=''
  or coalesce(current_setting('mcmods.audit_until',true),'')=''
  or coalesce(current_setting('mcmods.audit_limit',true),'') !~ '^[0-9]{1,4}$' then
  raise exception 'AUDIT_CATALOG_ACTIVITY_ARGUMENT_REQUIRED' using errcode='22023';
 end if;
 if current_setting('mcmods.audit_since')::timestamptz>=current_setting('mcmods.audit_until')::timestamptz
  or current_setting('mcmods.audit_until')::timestamptz-current_setting('mcmods.audit_since')::timestamptz>interval '31 days'
  or current_setting('mcmods.audit_limit')::int not between 1 and 1000 then
  raise exception 'AUDIT_CATALOG_ACTIVITY_SCOPE_INVALID' using errcode='22023';
 end if;
end $scope$;
select set_config('search_path',quote_ident(current_setting('mcmods.audit_schema'))||',pg_catalog',true);
with p as materialized (
 select current_setting('mcmods.audit_actor') actor,current_setting('mcmods.audit_target') target,
  current_setting('mcmods.audit_since')::timestamptz since,current_setting('mcmods.audit_until')::timestamptz until,
  current_setting('mcmods.audit_object_code') object_code,current_setting('mcmods.audit_limit')::int cap
), actor as materialized (select u.id from users u,p where u.public_id=p.actor),
target as materialized (select route.id,route.internal_id,route.entity_type from public_routes route,p where route.public_id=p.target),
tasks as materialized (
 select task.task_uid,task.status,task.created_at from ai_tasks task,actor,p
 where task.created_by=actor.id and task.task_type='content_translation_completion' and task.payload->>'publicId'=p.target
  and task.created_at>=p.since and task.created_at<p.until order by task.created_at,task.id limit (select cap from p)
), activities as materialized (
 select event.id from user_activity_events event,actor,target,p
 where event.user_id=actor.id and event.object_route_id=target.id
  and event.action_id=(select id from activity_actions where code='create')
  and event.object_type_id=(select id from activity_object_types where code=p.object_code)
  and event.occurred_at>=p.since and event.occurred_at<p.until order by event.occurred_at,event.id limit (select cap from p)
), queued as materialized (
 select intent.id from activity_event_outbox intent,actor,target,p
 where intent.user_id=actor.id and intent.action_id=(select id from activity_actions where code='create')
  and intent.object_type_id=(select id from activity_object_types where code=p.object_code)
  and (intent.object_route_id=target.id or intent.object_public_id=p.target or (intent.object_entity_type=target.entity_type and intent.object_internal_id=target.internal_id))
  and intent.occurred_at>=p.since and intent.occurred_at<p.until order by intent.occurred_at,intent.id limit (select cap from p)
), totals as (
 select (select count(*) from tasks) tasks,(select count(*) from activities) activities,(select count(*) from queued) queued,
  exists(select 1 from actor) and exists(select 1 from target) scope_known,
  (select count(*) from tasks)>=(select cap from p) or (select count(*) from activities)>=(select cap from p) or (select count(*) from queued)>=(select cap from p) possible_truncation
)
select jsonb_build_object('dry_run',true,'repair_supported',false,'transaction_read_only',current_setting('transaction_read_only'),
 'database',current_database(),'schema',current_schema(),'actor_public_id',(select actor from p),'target_public_id',(select target from p),
 'declared_activity_object_code',(select object_code from p),'scope_known',totals.scope_known,'possible_truncation',totals.possible_truncation,
 'tasks',totals.tasks,'retained_activity_rows',totals.activities,'queued_activity_intents',totals.queued,
 'count_difference_candidate',case when totals.scope_known and not totals.possible_truncation then totals.tasks<>totals.activities+totals.queued else null end,
 'historical_attribution_proven',false,
 'task_rows',coalesce((select jsonb_agg(to_jsonb(tasks) order by created_at,task_uid) from tasks),'[]'::jsonb),
 'interpretation','Counts require request/task correlation. Legitimate catalog creation, retention, current actor/object mapping and observation-window edges can differ. No entitlement, loss, duplicate payout or recipient delivery is proved; do not reconstruct activities or debit balances/XP.') as diagnosis from totals;
rollback;
