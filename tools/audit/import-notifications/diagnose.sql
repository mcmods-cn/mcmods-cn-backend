-- Explicit bounded read-only diagnostic. No repair/replay/apply statements.
\set ON_ERROR_STOP on
\set VERBOSITY verbose
begin transaction isolation level repeatable read read only;
set local statement_timeout='30s';
set local lock_timeout='3s';
set local search_path to pg_catalog;
\if :{?expected_database}
\else
  \echo 'expected_database is required'
  do $$begin raise exception 'AUDIT_IMPORT_NOTIFICATION_ARGUMENT_REQUIRED' using errcode='22023';end$$;
\endif
\if :{?audit_schema}
\else
  \echo 'audit_schema is required'
  do $$begin raise exception 'AUDIT_IMPORT_NOTIFICATION_ARGUMENT_REQUIRED' using errcode='22023';end$$;
\endif
\if :{?project_public_id}
\else
  \echo 'project_public_id is required'
  do $$begin raise exception 'AUDIT_IMPORT_NOTIFICATION_ARGUMENT_REQUIRED' using errcode='22023';end$$;
\endif
\if :{?since}
\else
  \echo 'inclusive RFC3339 since is required'
  do $$begin raise exception 'AUDIT_IMPORT_NOTIFICATION_ARGUMENT_REQUIRED' using errcode='22023';end$$;
\endif
\if :{?until}
\else
  \echo 'exclusive RFC3339 until is required'
  do $$begin raise exception 'AUDIT_IMPORT_NOTIFICATION_ARGUMENT_REQUIRED' using errcode='22023';end$$;
\endif
\if :{?max_jobs}
\else
  \echo 'max_jobs (1..10000) is required'
  do $$begin raise exception 'AUDIT_IMPORT_NOTIFICATION_ARGUMENT_REQUIRED' using errcode='22023';end$$;
\endif

select current_database()=:'expected_database'
  and :'audit_schema' ~ '^[a-z_][a-z0-9_]{0,62}$'
  and :'project_public_id' ~ '^[a-z0-9]{9}$'
  and :'since'::timestamptz < :'until'::timestamptz
  and :'until'::timestamptz-:'since'::timestamptz <= interval '31 days'
  and :'max_jobs'::integer between 1 and 10000 as audit_scope_ok \gset
\if :audit_scope_ok
\else
  \echo 'database mismatch or invalid bounded scope; no diagnostic query executed'
  do $$begin raise exception 'AUDIT_IMPORT_NOTIFICATION_SCOPE_MISMATCH' using errcode='22023';end$$;
\endif

set local search_path to :"audit_schema",pg_catalog;

with selected as materialized (
  select job.id,job.status,job.created_at,job.updated_at,
    job.created_by is not null as has_current_recipient,
    case job.status when 'ready' then 'mod_import_success'
      when 'partial' then 'mod_import_partial'
      else 'mod_import_failure' end as expected_template
  from catalog_import_jobs job join mods mod on mod.id=job.mod_id
  where mod.project_code=:'project_public_id'
    and job.status in ('ready','partial','failed')
    and job.updated_at >= :'since'::timestamptz
    and job.updated_at < :'until'::timestamptz
  order by job.updated_at,job.id limit :'max_jobs'::integer
), retained as materialized (
  select selected.*,
    (select count(*) from nats_outbox intent
     where intent.event_type='notification.direct'
       and intent.payload->'data'->>'jobId'=selected.id
       and intent.payload->>'templateKey'=selected.expected_template) as retained_matching_intents,
    (select count(*) from notifications notification
     where notification.data->>'jobId'=selected.id
       and notification.template_key=selected.expected_template) as retained_matching_notifications
  from selected
), inspected as (
  select retained.*,
    has_current_recipient and retained_matching_intents=0
      and retained_matching_notifications=0 as missing_retained_result_candidate
  from retained
)
select jsonb_pretty(jsonb_build_object(
  'dry_run',true,'repair_supported',false,'transaction_read_only',current_setting('transaction_read_only'),
  'database',current_database(),'schema',current_schema(),
  'project_public_id',:'project_public_id','since',:'since','until',:'until','max_jobs',:'max_jobs'::integer,
  'inspected_jobs',(select count(*) from inspected),
  'possible_truncation',(select count(*)>=:'max_jobs'::integer from inspected),
  'missing_retained_result_candidates',(select count(*) from inspected where missing_retained_result_candidate),
  'jobs',coalesce((select jsonb_agg(to_jsonb(inspected) order by updated_at,id) from inspected),'[]'::jsonb),
  'interpretation','Absence is a candidate, not proof of lost delivery. Retention, deleted accounts and repeated job attempts require independent historical correlation. Presence cannot prove the current attempt or recipient receipt. Do not resend notifications or replay rewards from these counts.'
));
rollback;
