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

For apply, review the diagnosis and add explicit `expectedDisplayEndpoint`,
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
source and stored bytes. The final transaction locks the original identity,
receipt, action and unchanged file snapshot, rechecks references and current
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
mutex and current receipt/file/action locks through bounded DELETE and HEAD.
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
