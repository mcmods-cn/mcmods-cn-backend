# Catalog tag prefix index and recovery

The export tag query filters `lower(canonical_id) LIKE '<prefix>%'` and an
optional exact `registry`, then uses a bounded registry/canonical-ID keyset cursor.
An ordinary folded B-tree does not provide a prefix range under every database
collation. The current prefix index gives PostgreSQL a `text_pattern_ops` range.
The repair command uses this concurrent build definition.

```sql
CREATE INDEX CONCURRENTLY idx_catalog_tags_canonical_prefix_registry
ON public.catalog_tags (lower(canonical_id) text_pattern_ops, registry, entity_id);
```

`GET /api/v1/export-revisions/{revisionId}/tags` treats `q` as a
case-insensitive literal prefix. `_`, `%` and `\` are literal characters,
not SQL wildcard syntax; for example, `q=example:foo_` matches
`example:foo_bar` but excludes `example:fooabar`. The loader escapes these
characters before adding its own trailing wildcard. The cursor continues to
bind the original query, registry and revision. Response limits and keyset
ordering are unchanged. No first-party export-tag query caller uses a wildcard
contract. This correction needs no schema change of its own.

The folded index and unique constraints serve equality and collation-aware
ordering queries. The prefix index adds storage and maintenance to tag writes.
The planner may still sort the bounded candidates. Representative test plans
cover the observed plans and sample data.

## Inspect and restore the index

Generation 169 installs the index in the baseline transaction. On startup,
the catalog fingerprint includes the index definition and its ready/valid
flags. Use the repair tool when the index is missing or a build was interrupted
in an otherwise matching installation. [Baseline installation](../README.md#数据库安装发布和恢复)

Build `go build -o ./db-index-repair ./cmd/db-index-repair`. The tool reads only
`MCMODS_INDEX_REPAIR_DATABASE_URL`, supplied privately by the operator, and
does not read dotenv or deployment defaults. Keep credentials out of command
arguments, shell history and reports. The following commands contain only an
exact confirmed database name and a new metadata path:

```sh
./db-index-repair
./db-index-repair -apply -confirm-database EXACT_DATABASE -backup NEW_PRIVATE_METADATA.json
```

Inspection is read-only and reports `exists`, `matches`, `ready` and `valid`.
Apply checks the exact configured and connected database names and the
generation 169 installation marker against the current compiled DDL. The tool
shares the baseline installer’s PostgreSQL advisory lock, using a session lock
because concurrent DDL runs outside a transaction.
Before any DDL, the tool durably creates a new mode-0600 metadata file containing
the prior index state, database name, generation and change identifier
`20261003_01`. It opens the metadata file exclusively. Preserve a separate
verified database backup for business-data recovery.

An already matching, ready and valid index is a no-op: it retains the same
index identity and does not create another metadata file. A same-named object
with a different table, expression, key order, operator class, collation,
predicate or sort option causes refusal. A generation marker is not proof of
whole-schema correctness; this tool verifies only its target index boundary.

## Interrupted builds

Plan for index-build disk/CPU/I/O, two table scans and waits for old transactions.
Concurrent building allows ordinary table writes but can wait on other schema
changes. The command has a ten-minute context bound and never disables safety
checks to force completion. Do not put concurrent DDL inside a transaction.

An interruption can leave an invalid or not-ready index. Inspect first, examine
the failed operation and confirm that no build is still active. A matching
invalid index is repaired only with explicit `-repair-invalid`, a new private
metadata path, the same exact database confirmation and no active build:

```sh
./db-index-repair -apply -repair-invalid -confirm-database EXACT_DATABASE -backup NEW_RECOVERY_METADATA.json
```

The tool concurrently removes only the matching invalid index and rebuilds it.
It refuses definition drift or an active build. If the rebuild fails, inspect
again; do not treat a successful process start or an existing index name as
success. Completion requires matching structure plus `indisready` and
`indisvalid`. The original folded/unique indexes and business rows are retained.

After repair, verify all four inspection flags and run
`go run ./cmd/db-initialize` on the installed database to verify its full
catalog fingerprint before returning it to service. Release rollback
uses the matching application and database snapshot. The current catalog
requires this index. The original 168 forward change and rollback instructions
remain in the [historical runbook](https://github.com/mcmods-cn/mcmods-cn-backend/blob/d14e10b4dcdeab3c7e2277dd371efa0249888eb8/docs/catalog-tag-prefix-index.md).

## Reproducible validation

The real PostgreSQL integration tests use nonce-owned child databases with the
complete generation 169 schema. They cover fresh installation,
preservation of fixture rows and existing indexes, repeated application, exact-target
and generation refusal, metadata failure before DDL, definition drift, an
actual concurrent writer/build barrier, cancellation leaving an invalid index,
active-build refusal, explicit recovery and the actual CLI's private metadata.

`TestOCT03CPERF010ActualTagPrefixQueryUsesInstalledIndexAtScaleIntegration`
captures the production loader's SQL and parameters, then checks its default
planner with 100,000 synthetic tags and 11 matching rows. It does not force a
planner setting. `TestOCT03CPERF010PublicExportTagAndAssetQueriesEnforceBoundedKeysetsIntegration`
checks public HTTP limits and keyset behavior separately from query plans.
`TestOCT03CPERF010TagSearchTreatsSQLWildcardsAsLiteralPrefixesIntegration`
checks URL-decoded literal `_`, `%` and `\` through the public handler and
records the actual production query plan on four synthetic tags. That small
sample proves matching semantics and bounds, not production performance.

```sh
GOMAXPROCS=2 python3 -B tools/testing/isolated_environment.py --state OWNED_STATE run -- \
  go test -race -p 1 ./internal/httpapi -run '^TestOCT03C(TagPrefixUpgrade|PERF010)' -v
```
