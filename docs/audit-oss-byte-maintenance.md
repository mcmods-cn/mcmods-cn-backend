# Scoped OSS byte diagnosis, promotion and input retention (CMP-005)

`cmd/oss-byte-maintenance` defaults to dry-run. It changes no schema, balances,
permissions, review decisions, reward history or public file IDs. This task has
not run it against production. Schema inspection/expansion is described in
`audit-oss-byte-binding-upgrade.md`; apply also requires its additive typed
maintenance journal. A missing journal is not created by a request or worker.

Use a private regular JSON plan and a new private report file. The CLI refuses
group/world-readable plans and creates reports exclusively with0600 permissions.
The private report contains observed hashes; console output contains only status
counts and read-byte totals. Neither output includes object keys, file contents,
credentials or signed URLs. Keep the plan and database journal/preimages private.

```json
{
  "version": 1,
  "database": "exact_operator_selected_database",
  "maxDurationSeconds": 300,
  "maxFiles": 1,
  "maxReadBytes": 1048576,
  "maxReferenceRows": 100000,
  "maxReferenceColumns": 2048,
  "maxReferenceFieldBytes": 4194304,
  "entries": [{
    "actionId": "11111111-1111-4111-8111-111111111111",
    "operation": "promote",
    "fileId": "exact_public_file_id"
  }]
}
```

```sh
go run ./cmd/oss-byte-maintenance --plan private-plan.json --report new-diagnosis.json
```

Diagnosis reads actual source/stored bytes with an explicit total byte budget.
It does not trust OSS hash metadata or legacy `source_hash_verified`. Missing
converted originals remain `MISSING_ORIGINAL_SOURCE`; no filename or converted
bytes are used to invent source proof. A supplied `originalSourceKey` must be the
same object or a recognized source-to-WebP/render path of the selected file.
Unknown reserved-key ownership, unsupported location and size/accounting changes
remain holds. Historical sender/uploader intent is never inferred from today's
bytes; `historicalIntentProven` remains false.

For apply, first establish the maintenance window described below. Review the diagnosis and add explicit `expectedDisplayEndpoint`,
`expectedSourceSha256`, `expectedStoredSha256`, `expectedSourceSize` and
`expectedStoredSize` pins. Provide a real operator-owned backup reference and
confirm the exact database separately:

```sh
go run ./cmd/oss-byte-maintenance --plan private-reviewed-plan.json \
  --report new-apply-report.json --apply --database exact_operator_selected_database \
  --backup-reference existing_operator_backup_identifier
```

Eligible legacy files are copied into new server-only UUID candidates, followed
by full actual GET/hash/length checks. Converted files keep separate original
source and stored bytes. The final transaction locks the original identity, the selected reference-write
tables, receipt, action and unchanged file snapshot, rechecks references and current
database-clock leases, and preserves the file/public ID, owner, names, category,
source, scan/review state and existing byte accounting. Old raw objects remain.
An explicitly pinned source/stored hash correction is recorded by the private
old-file preimage and new typed proof; no historical evidence is overwritten.

The initial promotion scope requires zero live file-FK and raw-key text/JSON/array
references. Shared/manual content with such references is held as
`NEEDS_SCOPED_REFERENCE_PLAN`; it is never bulk-rewritten or deleted. Reference
inventory discovers quoted identifiers from public database metadata, tests
literal and escaped-key variants, and respects explicit column/row/field-size
budgets and a2s statement budget. An incomplete scan is a hold, not zero matches.
Reported matches count FK edges and matching cells, not distinct entities.
An old typed receipt with a missing display-endpoint snapshot can instead be
restored after actual source/stored verification and an explicit endpoint pin;
this keeps keys and shared references unchanged.

For input retention, select exact committed `bindingId` values with
`operation: "cleanup_raw"` and an explicit top-level `retentionCutoff` at least
seven days in the past. The receipt must predate it. Default `rawSourceKind`
selects the original incoming key; `legacy_original` selects only an original
source recorded by a completed converted promotion, never an arbitrary key.

Cleanup requires a current active source/stored proof, actual retained
compensation bytes and an exact raw-byte match. Registered objects, preparing
attempts, unfinished/dead multipart sessions, reservations and live raw-key
references block it. File-ID references to the adopted sealed file remain valid.
A dedicated durable-intent executor repeats these guards under the original-key
mutex, selected reference-write table locks and current receipt/file/action locks
through bounded DELETE and HEAD. The scan happens after these locks are acquired.
Completion means the current object is absent. It does not erase old provider
versions or promise physical byte reclamation for a versioned bucket. Existing
versioning, encryption and bucket lifecycle policies are not changed.

Normal preparation uses the same short original-key mutex and refuses retired
inputs; it retains no connection during copy/read/hash. Ordinary completed
replay and owner-authorized original aliases read sealed bytes after retention.
Same action ID/plan is idempotent; changed entries, database or global budgets
conflict. Dry-run may report unresolved proof without changing data; apply holds
and execution errors exit nonzero after writing the private report. An already
absent input is recorded as retired on apply without claiming a deleted object. Expired action owners
cannot commit. Interrupted candidates use the existing exact receipt recovery;
interrupted cleanup stays durable and can be retried under the same reviewed
plan after failure/lease expiry.

Keep journal/preimages, receipts and sealed source/stored bytes for compensation.
Do not roll back to an unverified raw key or blanket-restore old trust flags.
Incorrect metadata/location needs a reviewed forward correction. A removed raw
input can be reconstructed from its recorded retained source or stored proof
under a separate authorized action; converted PNG/WebP proofs are not
interchangeable. Production execution and any reference migration require their
own concrete scoped review; this command supplies no blanket cleanup policy.


## Required maintenance window and reference scope

This data tool is an offline, scoped maintenance operation. Before apply, the
operator must pause all application, Worker, administrator and SQL writers that
can create or change references in the affected scope, and pause related DDL.
Keep them paused from before the final reviewed diagnosis through apply and its
post-checks. The tool cannot verify that operational pause. The report always
sets `onlineRaceSafe: false`; an operator declaration is not a database lock or
proof of universal online safety.

Dry-run does not acquire write-conflict table locks. Its private
`referenceScope` lists `potentialReferenceTables` and a
`referenceSchemaSha256` fingerprint of the discovered public text/JSON/array/
domain columns, table identities and inheritance topology. Live file foreign
keys are also inspected for relocation. Control/history tables are explicitly
excluded and their applicable lease/session/reservation states are checked
separately. Non-public or externally encoded references require separate
inventory and the same paused writers; this command does not claim to discover
every arbitrary representation of an asset identifier.

After reviewing that inventory, add these fields to the apply plan:

- `referenceSchemaSha256`: the exact reviewed fingerprint;
- `referenceWritesQuiesced: true`: the operator's actual related-writer and DDL
  pause, not permission for the tool to stop services;
- `referenceWriteTables`: at most16 named public reference tables, including
  `oss_files`, whose writers the tool will additionally block with database
  `SHARE ROW EXCLUSIVE` locks;
- `quiescedReferenceTables`: each remaining discovered potential reference
  table by its exact name, whose writers remain paused operationally.

The two lists cannot overlap. An uncovered table is
`REFERENCE_WRITE_SCOPE_GAP`; an unreviewed metadata change is
`REFERENCE_SCHEMA_CHANGED`. Both hold apply before a durable action is created.
Foreign relations cannot enter the actual lock list. Partition descendants are
expanded and counted, with a total cap of16 physical tables including parents;
a larger or cross-schema expansion holds before taking partial table locks.
This is deliberately not a whole-database lock. Every listed but unlocked table
still depends on the operator pause, even when `mutationPreconditionsMet` is
true. That field records checked plan prerequisites, not observed quiescence.

The final executor acquires the original identity, migration-coordination lock,
selected table locks and receipt/action/file locks in a fixed order with a
shared5s acquisition deadline, then repeats the inventory and reference scan
inside the locks. The total final critical section is at most30s, provider
DELETE+HEAD at most20s, and each reference SQL statement at most2s. Normal
writers that straggle into selected tables can wait for that critical section;
lock failures/timeouts hold or fail the action rather than broadening the lock
scope or waiting forever. Unlocked writers must remain paused throughout.

The journal records the reviewed fingerprint and operator pause assertion
separately from `write_fence_tables`, the actual physical table locks completed
by a successful final executor. Old journal rows gain blank/false/empty defaults
only during explicit additive schema expansion; they acquire no inferred past
pause or fence proof. Normal upload startup does not require this optional
journal expansion. Old plan digests are preserved: do not silently reuse an old
action ID with the new scope or discard its intent; review its state and use an
explicit new action where appropriate.

A provider DELETE and a database transaction cannot be rolled back atomically.
If DELETE succeeds but HEAD/commit fails, the durable intent remains failed or
processing, the retained typed compensation bytes remain, and the same exact
reviewed plan can reconcile an already-absent raw input. An expired/superseded
owner cannot commit. Keep the maintenance pause until those postconditions are
checked. A later compensating restore requires a separate authorized action;
the tool never promises to reconstruct old provider versions or delete bytes
already downloaded by another party.
