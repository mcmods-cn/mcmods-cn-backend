-- See docs/load-testing.md for the isolated dataset and required psql arguments.
\set ON_ERROR_STOP on
\if :{?expected_database}
\else
  do $$begin raise exception 'expected_database is required';end$$;
\endif
\if :{?representative_user_id}
\else
  do $$begin raise exception 'representative_user_id is required';end$$;
\endif
\if :{?representative_action}
\else
  do $$begin raise exception 'representative_action is required';end$$;
\endif

begin read only;
select current_database()=:'expected_database' as expected_database_matches \gset
\if :expected_database_matches
\else
  do $$begin raise exception 'connected database does not match expected_database';end$$;
\endif
set local search_path to pg_catalog, public;
select exists(select 1 from users where id=:'representative_user_id'::bigint)
  and length(btrim(:'representative_action'))>0 as plan_arguments_valid \gset
\if :plan_arguments_valid
\else
  do $$begin raise exception 'choose an existing internal user and a nonempty anti-abuse action';end$$;
\endif

-- Individual user/action index probes. Middleware also matches other scopes.
explain (analyze, buffers, costs true, format text)
select id from anti_abuse_events
where action=:'representative_action'
order by created_at desc,id desc limit 100;

explain (analyze, buffers, costs true, format text)
select id from anti_abuse_content_fingerprints
where action=:'representative_action' and user_id=:'representative_user_id'::bigint
  and created_at>now()-interval '24 hours'
order by created_at desc,id desc limit 200;

explain (analyze, buffers, costs true, format text)
select id from anti_abuse_restrictions
where user_id=:'representative_user_id'::bigint and lifted_at is null and starts_at<=now()
  and (ends_at is null or ends_at>now())
order by starts_at desc limit 1;
rollback;
