-- Execute with EXPLAIN (ANALYZE, BUFFERS) only on disposable/staging data.
explain (costs true, buffers false)
select id from anti_abuse_events
where action='comment.create'
order by created_at desc,id desc limit 100;

explain (costs true, buffers false)
select id from anti_abuse_content_fingerprints
where action='comment.create' and user_id=1
  and created_at>now()-interval '24 hours'
order by created_at desc,id desc limit 200;

explain (costs true, buffers false)
select id from anti_abuse_restrictions
where user_id=1 and lifted_at is null and starts_at<=now()
  and (ends_at is null or ends_at>now())
order by starts_at desc limit 1;
