-- Run with psql only on disposable/staging data after loading representative
-- high-cardinality anti-abuse history. Example:
-- psql ... -v representative_user_id=42 -v representative_action=comment.create -f scripts/anti-abuse-query-analysis.sql
\if :{?representative_user_id}
\else
  \echo 'representative_user_id is required'
  \quit
\endif
\if :{?representative_action}
\else
  \echo 'representative_action is required'
  \quit
\endif

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
