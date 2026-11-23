# 2026-10-24 ingestion repairs

This change is an implementation batch, not a release approval. Original V2
observations and the 175-scenario specification remain historical inputs.
Use the current isolated environment guide for validation on a later source revision.
[Current validation guide](../testing-isolated-environment.md)

- GR-007: cancellation and retry use the original creator; deduplicated creation
  cannot take over another creator's task. Transactional outbox remains atomic.
- GR-002: a published import cannot be rejected through the pending-review API.
  An explicit publication withdrawal operation requires a separate contract.
- GR-006: candidate activation locks the common target version before revision
  rows, serializing replacement and avoiding the empty-active-set uniqueness race.
- GR-004/GR-008: reads, preview, queue actions use existing global/scoped review
  policy; import detail identifies its source package, target, counts and comparison
  revision, rather than pretending to be a project-metadata revision.
- GR-R01: the Worker reports progress before entering the write transaction;
  flushing cannot reacquire its own connection pool through the progress callback.
- GR-003: overwrite=false preserves existing imported values and missing entries,
  adds new fields/resources and preserves human content; overwrite=true replaces
  and reconciles the import-owned scope. Human projections remain authoritative.
- GR-005: import activity user parameters are explicitly bigint.
- CONT-002: public recipe list internal identity is scanned as int64, matching the
  authoritative rendering projection. CONT-004 validates both canonical flattened
  numeric properties and the legacy nested input before canonicalization.
- GR-009: malformed explicit quantities and contradictory probabilities fail;
  absence keeps the documented default rather than fabricating a replacement value.
- F-002: categories v6 and v7 are explicit supported schemas. v7 full/templates_only
  and omission counts are validated; template/recipe collection versions remain v2.
  The checked-in JEI fixtures are **synthetic producer-contract samples**, not game
  outputs. Unknown versions remain rejected. v6 carrying v7 omission semantics is
  rejected rather than silently treating an omitted collection as complete.
- F-003: single-project website import rejects all_namespaces=true with an
  actionable explicit-namespace message; standalone exporter all mode is retained.

GR-001 is only partially repaired here: PNG text/EXIF/unregistered ancillary
chunks are removed while encoded image, alpha and color chunks are preserved;
manifest diagnostics, capabilities, nested advancement display and chance labels
receive purpose-specific projections. **Unregistered JSON attachments and partial
raw-source fallback remain unresolved**. Neither this commit nor its passing tests
claim complete GR-001 remediation. Unknown business extensions must receive a
registered purpose or an explicit product decision, not blanket deletion.

The full HTTP fixture uses real PostgreSQL and actual business Worker code with a
synthetic local OSS provider. Authentication, permissions and anti-abuse remain
active. It uses ordinary verified-email 30-day-old users with scoped permissions,
not administrators. It does not prove a browser flow, real OSS, NATS delivery or a
Minecraft export. The isolated-schema helper installs the actual migration
statements in an owned ordinary schema because PostgreSQL does not resolve
unqualified functions from pg_temp. Cleanup checks both owner and random marker.

Historical command from the isolated checkout used for this batch.

```sh
source /workspace/.cloud-setup/activate.sh
MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'TestAuditFix(Game|JEI)|TestAudit(GR|CONT)|TestModExportRetryAndRecoveryCreateAtomicOutboxAttempts|TestImportedContentSyncReconcilesOnlyItsSourceScopeIntegration' -count=1 -v
```

Original fixture/rate-limit failures and subsequent results are retained in the
portable task evidence under logs/backend_ingestion/. The retry fixture now calls
as its actual owner, tests an unrelated actor separately, and preserves outbox
failure rollback assertions. The projection reconciliation fixture now runs both
overwrite modes; it no longer asserts the historical behavior that ignored false.

No production data, balances or assets were modified. Old leaked data requires a
separate scoped diagnostic and migration/compensation decision.
