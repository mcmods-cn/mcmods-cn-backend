# Catalog translation activity candidate counts

Read `diagnose.sql` first. This operator-only tool uses one declared database,
schema, public actor, public target, reviewed activity-object code and at most
31 days/1000 retained rows per family. It runs READ ONLY REPEATABLE READ with
10s statements/2s locks and rolls back. It never selects bodies, credentials,
email, token payloads, balances or reward amounts and has no apply mode.

Supply connection-local settings, for example an authorized isolated database:

```sh
PGOPTIONS='-c mcmods.audit_database=mcmods_audit_test -c mcmods.audit_schema=public -c mcmods.audit_actor=example01 -c mcmods.audit_target=example02 -c mcmods.audit_object_code=mod -c mcmods.audit_since=2026-10-24T00:00:00Z -c mcmods.audit_until=2026-10-25T00:00:00Z -c mcmods.audit_limit=100' \
  psql "$AUDIT_TEST_DATABASE_URL" -X -qAt -v ON_ERROR_STOP=1 -f tools/audit/catalog-translation-activity/diagnose.sql
```

Unknown actor/target is explicit `scope_known=false`; a truncated family makes
the count candidate unknown. Database mismatch raises the exact22023 guard
before business queries. The caller supplies the activity-object code from the
reviewed producer version; current mapping must not invent historical policy.
`historical_attribution_proven` is always false. A difference only selects a
review candidate: normal object creation, cleanup, different event times and
retention may explain it. Existing ledger and request/event evidence must settle
attribution before any separately authorized compensation. No historical repair
or production diagnosis has been executed.
