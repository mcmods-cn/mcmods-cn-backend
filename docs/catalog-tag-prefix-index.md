# Catalog tag prefix index: forward change 20261003_01

The export tag query filters `lower(canonical_id) LIKE '<prefix>%'` and an
optional exact `registry`, then uses a bounded registry/canonical-ID keyset cursor.
An ordinary folded B-tree does not provide a prefix range under every database
collation. This additive index gives PostgreSQL a `text_pattern_ops` range:

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

The existing folded index and unique constraints remain. This new index adds
storage and maintenance to tag writes; it does not replace collation-aware
sorting. The planner may still sort the bounded candidates. Representative
test plans do not establish production latency, throughput or database health.

## Fresh and existing databases

Fresh schema installation includes the unqualified index in its installation
transaction, so temporary-schema tests also receive it. The schema generation
stays **168**. Startup of an already installed generation-168 database does
**not** automatically apply this change, and such a database remains usable.
Installations with a missing index need the explicit tool for the prefix
performance improvement. Do not reset an existing database or edit old schema
history to obtain it.

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
Apply requires the configured and connected database names to match the exact
confirmation and checks generation 168. It holds the existing session-level
schema-migration advisory lock. The concurrent DDL runs outside a transaction.
Before any DDL, the tool durably creates a new mode-0600 metadata file containing
the prior index state, database name and generation. Existing files are never
overwritten. This is **operator metadata, not a backup of business data**.

An already matching, ready and valid index is a no-op: it retains the same
index identity and does not create another metadata file. A same-named object
with a different table, expression, key order, operator class, collation,
predicate or sort option causes refusal. A generation marker is not proof of
whole-schema correctness; this tool verifies only its target index boundary.

## Deployment, interruption and recovery

Run the explicit additive change before or alongside deploying this release.
Existing application code remains compatible; there is no field backfill,
data rewrite or feature switch. The operator must arrange a verified data
backup according to the deployment procedure independently of this metadata.
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

If a rollback is required, first confirm the exact target and reviewed index
definition, then an authorized operator can remove only this additive index
with `DROP INDEX CONCURRENTLY public.idx_catalog_tags_canonical_prefix_registry`.
This removes the new performance support, not data. No production DDL was run
as part of this audit.

## Reproducible validation

The real PostgreSQL integration tests use nonce-owned child databases with the
complete schema, rather than a reduced mock. They cover fresh installation,
old generation-168 data and retained indexes, repeated application, exact-target
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
