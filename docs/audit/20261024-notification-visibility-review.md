# Notification visibility review, 2026-10-24

This is a named NEXT-001 / MSG-001 implementation and regression scope. It is
not a whole-project release verdict. Production execution has not occurred.

The read-all watermark records a PostgreSQL snapshot and each notification's
revision records its top-level transaction ID. Candidate unread broadcasts are
selected by changed time, post-snapshot XIDs and snapshot-active XIDs. The three
sets are disjoint. Candidate scans retain their indexed time/XID order before
outer ID constraints; the per-user broadcast baseline is materialized once.
An unrelated long-running transaction does not expand the XID candidates into
all history. No query timeout, connection pool or original performance budget
was increased.

A real 16-user, snapshot-watermark test initially exhausted the existing
five-minute context at 10M rows. At 100k and 1M, plans showed 256 full historical
prefix scans (1,836.769 ms and 22,858.911 ms). An intermediate change passed the
elapsed-time gate but still scanned history and was retained as insufficient.
A separate before/after comparison uses the frozen 480cf76 query and identical
complete indexes for both sides: the old query still filters the 100k-row prefix
256 times, so the first fixture's missing legacy time index is not used as sole
attribution evidence. The corrected query does not filter the old prefix.

Final fixed-resource validation used 4 cgroup CPUs / 16 GiB / PostgreSQL 18.6:

- Snapshot-watermark 100k / 1M / 10M, with an unrelated old transaction kept open:
  all counts, single-query and original plan gates passed in 22.52 s total.
  Individual truth calls: 361.05 / 326.29 / 340.42 ms; plans are dominated by JIT,
  with indexed candidate scans rather than historical row filtering.
- Existing 1M notification pagination/read-all: passed in 5.20 s.
- Existing no-watermark 100k / 1M / 10M sampled-unread test: passed in 21.49 s.
- Actual HTTP + PostgreSQL + directly invoked NotificationWorker commit
  barriers cover late personal inserts, late aggregation and late broadcasts,
  duplicate delivery, per-user receipts and unread summary counts. These tests
  do not claim queue-transport or browser coverage.

## Explicit schema upgrade

The additive generation-168 upgrade now includes the required partial covering
index `idx_notifications_broadcast_revision` on `(revision_xid,id)` including
`updated_at`, for rows with `recipient_id is null`. Startup verifies the exact
index, column types and notification revision-trigger binding/body. It performs
no startup DDL. Existing manually declared scale fixtures include the same real
indexes; original count/size/query/plan assertions remain in place.

`go run ./cmd/db-notification-visibility-upgrade` is read-only diagnosis by
default. `--apply` requires the exact `--database` and a verified
`--backup-reference`. The command pins `public,pg_catalog`, rejects incompatible
existing types, applies DDL under the migration advisory lock, checks real
postconditions before commit and verifies again afterward. A same-named wrong
index cannot produce a successful result. No count is hardcoded as a success.

Deploy the explicit schema upgrade before the new backend. Index creation is
transactional and may hold write-blocking locks while scanning existing rows;
choose a maintenance window from the actual data size. On DDL failure the
transaction rolls back. Downgrade the backend before removing the required
index; keeping additive columns/indexes preserves history and is the preferred
code rollback. Do not remove revision/snapshot fields merely to roll back code.
Previously misclassified historical read state cannot be reconstructed from the
new fields alone; the command reports that limitation.

Task evidence preserves the original failure, intermediate attempts and final
passes in `logs/messaging-review/`, including frozen-query/source provenance.
Further receipt-race work, if present, is tracked separately from this scale fix.

## Receipt ordering follow-up

Two additional reachable follower-aggregation races were reproduced with real
HTTP and PostgreSQL barriers. An old single-read statement could write its old
revision receipt after the aggregation committed. A later read-all then failed
to cover the now-visible revision. Two single-read statements could also finish
in reverse order and replace the newer receipt with the old revision marker.
Both original failures remain in the task evidence.

The common notification unread predicate now governs page flags, explicit-read
eligibility, direct truth counts and follower aggregation selection. A covering
read-all snapshot can supersede an obsolete non-null receipt. Existing
`read_at=NULL` explicit-unread markers retain their previous precedence, and
current per-item reads can clear them. Broadcast correction uses the same rule.
The single-read conflict branch checks whether the existing marker's revision
was visible to the older statement snapshot before replacing it; it does not
compare numeric XID maxima. Actual row-version, list and summary assertions
cover both races, plus preserved NULL markers and user isolation.
