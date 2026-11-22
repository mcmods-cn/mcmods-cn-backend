# MSG-001 / MSG-002 / MSG-003 / NEXT-001

> 本文的升级步骤适用于 generation 168，命令源码见 `d14e10b4`。新库的通知约束随[当前基线](../README.md#数据库安装发布和恢复)安装。

Notification read-all now captures a PostgreSQL `pg_snapshot`. Each notification
revision records its top-level transaction as `xid8`; a single-item receipt
records the exact observed revision. Sequence allocation and an update's wall
clock time cannot prove that the update had committed before read-all. Both the
list and unread-count projection use the same snapshot/version rules. Aggregate
updates after a single-item receipt become unread again.

Redis unread hashes carry format `version=2`. Older derivatives are ignored and
rebuilt from PostgreSQL on first read; delta adjustment cannot promote an old
derivative. No `FLUSHDB` or unrelated cache deletion is necessary.

SSE admission requires its configured Redis lease and a bounded PostgreSQL lease.
PostgreSQL preserves the active budget across a volatile Redis restart. A single
transaction lock serializes admission, including total/user/session budgets;
heartbeats extend leases and disconnect if either dependency fails. Disconnect
releases both leases; after a process crash the lease expires after 90 seconds.
The table is bounded by the existing 2,048-connection deployment budget and stale
rows are pruned at admission. Explicit single-instance optional/disabled Redis
operation remains available and also uses the database budget. Required Redis
(or configured fail-closed/shared operation) failure returns `/ready` 503;
optional single-instance caching reports a degraded dependency without making
the process unready.
`/live` keeps its independent process-alive meaning.

## Installation and upgrade

Fresh generation-168 installation includes the additive extension. Existing
generation-168 installations must explicitly upgrade before starting this code.
Coordinate the identity/OSS and catalog-import policy extensions documented in
their own files. Startup checks all required extensions and refuses to run with
an incomplete schema.

1. On an authorized copy, run `go run ./cmd/db-notification-visibility-upgrade`.
   The default command is read-only and reports schema counts, not notification
   text or recipient credentials.
2. Before an actual deployment, back up the target database and verify recovery.
   Stop old API/notification writers and drain workers so older read semantics
   cannot write new receipts while the new version is rolling out.
3. Run `go run ./cmd/db-notification-visibility-upgrade --apply --database EXACT_DATABASE --backup-reference VERIFIED_BACKUP_REFERENCE`.
   The reference is an operator assertion, not a backup created or verified by
   this command. The command checks the exact database and generation, obtains
   the shared schema lock, and expands in one transaction. Repeating it is safe.
4. Start this backend and workers, verify `/ready`, and run the read/notification
   and two-instance SSE checks. Then publish the matching frontend version.

Existing notifications and IDs are retained. Existing read receipts are stamped
with their current retained revision; legacy read-all rows retain their prior
timestamp/ID fallback until the next read-all captures a snapshot. The database
does not contain enough evidence to reconstruct which historical notifications
were swallowed by the old rule. The upgrade does not invent unread state or
rewrite notification content. Diagnose those historical cases separately against
retained delivery/request evidence.

Keep all added columns and the lease table on application rollback. Do not drop
the table while connections are alive or lower the TTL to reclaim slots early.
Reverting to the vulnerable backend restores the original defect and is not a
security fix. Prefer a forward correction; a maintenance rollback requires all
SSE connections to drain and all writers to stop. No production migration or
production data repair has been executed by this task.

## Evidence

`TestAuditNotificationLateInsertRemainsUnreadAfterWatermark` and
`TestAuditNotificationLateAggregationRemainsUnreadAfterWatermark` use controlled
PostgreSQL commit barriers and mounted HTTP APIs. The same tests fail at the
frozen baseline and pass after this change. Other checks cover duplicate delivery,
user isolation, concurrent single/all reads, retained-data upgrade/idempotence,
one-million-row original pagination budgets and ten-million-row broadcast counts.

`TestAuditMSG001And003RealRedisTwoHTTPInstancesColdRestart` starts an owned real
Redis executable, two real HTTP servers and PostgreSQL. It stops and cold-starts
Redis without persistence, checks readiness/liveness, rejects additional leases
while established leases remain, and verifies release/reconnection. Set
`MCMODS_TEST_REDIS_BINARY` explicitly; a skipped test is not verification.

MSG-002 returns 404 only for `pgx.ErrNoRows`; database failure returns 500 before
creating a conversation. Its database-unavailable component counterexample and
the existing full HTTP/private-messaging regressions are separate evidence layers.
