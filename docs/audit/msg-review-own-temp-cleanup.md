# Notification scale fixture: synchronous own TEMP cleanup

This is a test-resource cleanup candidate, not a product-policy change or a
claim that all historical PostgreSQL 53100 failures have been explained.

The c86 complete run retained a 53100 while installing the next fixture after
`TestAuditMsgReviewSnapshotWatermarkUnreadSampleScaleIntegration` passed its
original 10M case. The scale test previously relied on closing its pool to
remove its temporary heap and indexes. Closing the client is not a statement
acknowledgement that the server has finished reclaiming its temporary files.

The candidate captures the original backend PID, temporary namespace/table OIDs,
table owner/current role, and `relpersistence`. Its exit path uses
`AcquireAllIdle` on the same one-connection pool, compares every original
identity value, then holds that
single connection through ordinary `DROP TABLE pg_temp.notifications` and a
same-backend absence check. A changed identity or a failed drop explicitly
fails cleanup. No idle original means an immediate error, without creating a
replacement connection. This table cleanup has no reconnect fallback, CASCADE,
process termination, manual checkpoint, or delay. Original old-XID rollback and old-pool close still
precede this cleanup; original main-pool close follows it.

The original 5-minute context, 100k/1M/10M inputs, one-query count, 16-user
sample, expected values, index and no-sequential-scan assertions are unchanged.
The cleanup uses the original context; an expired context fails explicitly.
It does not lengthen a query, test context or package alarm.

Three new real-PG tests are proposed: changed original identity sends zero DROP
and preserves the original table/sentinel; a dependent own TEMP view produces
the exact 2BP01 instead of using CASCADE; successful own TEMP removal preserves
another own TEMP table and is followed by installing a fresh actual generation
168 schema. Each new test has a 30-second context; only that new schema's
ordinary owned cleanup has an additional bounded 10-second cleanup context.

All executions remain **NOT_RUN** until the independent source review and the
root's resource window. Use a new nonce-owned initialized PostgreSQL test DB,
with identical `DATABASE_URL` and `MCMODS_TEST_DATABASE_URL`, `APP_ENV=test`,
`MCMODS_RUN_DB_INTEGRATION=1`, `GOFLAGS=-p=1`, `GOMAXPROCS=2`, `CGO_ENABLED=1`.
Normal then race commands (proposed, not executed):

```sh
go test -json -p=1 -parallel=1 -timeout=10m -count=1 ./internal/httpapi -run '^TestAuditMsgReviewTempCleanup.*Integration$'
go test -json -race -p=1 -parallel=1 -timeout=10m -count=1 ./internal/httpapi -run '^TestAuditMsgReviewTempCleanup.*Integration$'
go test -json -p=1 -parallel=1 -timeout=10m -count=1 ./internal/httpapi -run '^(TestAuditMsgReviewSnapshotWatermarkUnreadSampleScaleIntegration|TestAuditMsgReviewLateCommitSummaryMatchesVisibleRevisionIntegration)$'
go test -json -race -p=1 -parallel=1 -timeout=10m -count=1 ./internal/httpapi -run '^(TestAuditMsgReviewSnapshotWatermarkUnreadSampleScaleIntegration|TestAuditMsgReviewLateCommitSummaryMatchesVisibleRevisionIntegration)$'
```

These targeted runs cannot rewrite the first failed complete c86 run or certify
a later complete run. Capacity and teardown timing still require actual new
evidence; a new full attempt must retain its own source, DB, log and footer.
