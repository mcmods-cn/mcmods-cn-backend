# GR-001: terminal original-upload retirement

An ordinary successful completion copies and hashes the original input into a
private server-owned key. Deleting or consuming that file previously queued
only the stored object and any converted private source. Its client upload key
could retain the original JSON/ZIP/image bytes indefinitely.

## Runtime contract

- Initial user/admin PUT, multipart part URLs, and export-upload resume persist
  the actual SDK `PresignResult.Expiration` before disclosing any signed URL.
  Multipart uses the maximum of every returned part; resume extends the maximum.
  `oss_upload_capabilities` stores identity/deadline/count only, never a URL,
  signed header, token, credential, or uploaded content. The API's default
  initial lifetime is 600s; the actual export-upload frontend explicitly asks
  for 3600s, and resume also uses 3600s. Each actual SDK deadline is retained.
- The ordinary file tombstone transaction queues a separate deletion intent for
  `upload_object_key`, linked by `raw_upload_binding_id` to the exact committed
  receipt. Its `next_attempt_at` is at least the real maximum deadline plus one
  second. Historical audit/package rows remain; the file remains tombstoned.
- Issuers, preparation, and final adoption check retirement under the existing
  original-key advisory lock. Retirement does not grant any source permission;
  ordinary actor/project/source-prefix checks run first. Re-signing a retired
  key returns 409 and requires a new upload.
- The actual deletion worker locks its queue lease, original-key identity and
  exact deleted file/receipt; it then reads a fresh DB clock after lock waits.
  It holds on unknown deadline/proof, active sharing/attempts/multipart uploads,
  or active import/proof references. Eligibility holds restore the attempt
  counter and do not exhaust provider-failure retry budgets.
- Streaming source-hash verification releases the DB connection. A second
  fenced transaction rechecks the receipt, current deadline and references,
  confirms a bounded exact-key multipart listing, and holds the lease/key
  through DELETE and exact provider 404 `NoSuchKey`. No nested connection is
  borrowed while that transaction is open. The normal 15s worker poll remains.
  Each row/key wait is followed by a new statement checking the actual deletion
  lease against the current DB clock; provider contexts are capped by the
  remaining lease. DELETE, HOLD, failure recording and completion cannot use an
  expired original token. Synthetic lease faults are defense tests, distinct
  from a demonstrated normal-user exploit.
- Report-evidence retention uses the same terminal transaction, including its
  current report status and immutable evidence metadata. Its existing day-based
  policy remains unchanged. The regression controls report eligibility alone;
  the actual source signature still expires at its real SDK-issued deadline.
- `raw_hold_reason` records eligibility holds separately from the unchanged
  closed `failure_class` provider-error domain. An unknown historical deadline
  remains pending with zero consumed failure attempts, rather than failing the
  provider-error constraint or becoming a false completed cleanup.

## Expansion, deployment, and rollback

Empty installations include the additive tables/column/FK/index. Existing
generation-168 databases require the existing read-only-by-default
`cmd/db-oss-byte-binding-upgrade` diagnosis and explicitly confirmed expansion.
There is no reset or inferred backfill of legacy signed capabilities.

Stop old upload issuers, resumers, and deletion workers before expansion. Expand
and verify the public schema, then deploy the new API/worker pair together.
An old deletion worker cannot interpret the typed raw intent and must never
consume it; an old issuer cannot record every writable authority. Mixed old/new
issuers or deletion workers are outside this compatibility window.

An existing pending generic intent for the same exact raw key can become typed.
If that old generic job is already processing, the pending-only scheduling guard
rolls back the whole tombstone transaction; retry after its owner finishes.
The compatibility path does not bypass that owner or its lease.

Rollback must first stop the new API/worker. Retain the additive schema and all
capability, file, receipt and outbox rows. Do not run an old deletion worker
while typed raw intents exist, and do not drop the retirement marker to permit
new signing/adoption. Resume with compatible code or an explicitly reviewed
operational plan; do not recreate deleted file rows or compensate balances.

## Existing data and physical boundaries

Production execution has not occurred. Already deleted/consumed historical
sources may have no real persisted signature deadline or typed byte proof.
Those are HOLD conditions, not permission to infer expiry from creation time,
the current TTL, metadata claims, or a receipt timestamp. The separate seven-day
active-file maintenance tool retains its original policy and is not repurposed.
Bounded read-only diagnosis must distinguish no intent, queued/held/completed
intent, unknown capabilities, and absent typed receipt; a DB state alone does
not prove provider absence.

The separate `cmd/oss-raw-retirement-diagnostic` accepts one to100 exact public
file IDs and a new private report path. It begins a repeatable-read, read-only
transaction pinned to public; it has no apply/delete operation and performs no
provider requests. It distinguishes unknown files, live files, no typed proof,
unknown capability, missing intent and each actual DB intent status. Multiple
receipts require explicit scope review. For example:

```
go run ./cmd/oss-raw-retirement-diagnostic --file-ids abc123def,ghi456jkl --report /private/new-raw-diagnostic.json
```

These example IDs are placeholders. A historical row with no retained real
capability or byte proof remains held; this diagnostic does not manufacture a
deadline or authorize production cleanup.

The automated tests use owned PostgreSQL and an owned SDK protocol provider.
They do not establish real-cloud versioning, retention/lifecycle policy, or
provider behavior for requests accepted before expiry that finish after a
DELETE. The service cannot retract bytes already downloaded by a third party.
Keep provider lifecycle/versioning and any exceptional in-flight request
reconciliation as separate operational acceptance evidence.

## Verification commands

Use the repository's owned-loopback DB variables and serialize short schema
setup/drop with the task PG mutex. The long expiry test releases that mutex
after setup, reclaims it for owned cleanup, and uses a schema ownership nonce.

```
go test ./internal/httpapi -run '^TestAuditGR001(DeletedCompletedUploadHasDurableOriginalRawCleanup|InitialUserAdminAndMultipartPersistActualCapabilityBeforeDisclosure|CapabilityPersistenceFailureDisclosesNoURLAndRollsBackTicket|ResumeExtendsActualCapabilityAndRetiredKeyRejectsWithoutSideEffects)Integration$' -count=1 -v
MCMODS_RUN_RAW_EXPIRY_INTEGRATION=1 go test ./internal/httpapi -run '^TestAuditGR001DefaultTicketRealExpiryRemovesOriginalPrivateBytesIntegration$' -count=1 -v -timeout=18m
```

The second command deliberately waits through the real default ten-minute
authority and normal polling. Its opt-in SKIP is never physical-cleanup PASS.
