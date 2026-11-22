# OSS upload byte binding and schema expansion (CMP-005)

> 本文的升级步骤适用于 generation 168，命令源码见 `d14e10b4`。新库使用[当前安装入口](../README.md#数据库安装发布和恢复)及[对象生命周期说明](oss-object-layout.md)。

New completions preserve the original incoming upload key as a stable alias and
adopt a separate key under the reserved `_mcmods_sealed_v1/<attempt UUID>/`
component. Client PUT and UploadPart signing refuse that component. A server
copy is followed by a full SHA-256 and length check of its actual bytes. Image
processing reads that sealed source; its result is separately hashed. The SDK
copier retains the2GiB ceiling, supports multipart copy and forwards the source
SSE/KMS and storage-class request settings. A successful COPY, ETag, version
identifier, or client metadata is never a hash proof.

Provider copy/read/process/sign operations hold no database transaction. A typed
receipt is persisted before the first candidate write, with a bounded renewable
lease. Renewal, conversion planning and adoption lock the row before checking the
current database clock in a separate statement. Original upload identity, rather
than a random candidate key, serializes competing valid completions. The final
transaction commits file, proof, quota settlement against the original key,
blueprint/report associations and the existing activity/outbox intent together.
Successful replays keep the same public file identity and contribute no second
upload event. Original-key user downloads/deletion and completion-result replays
resolve the alias before their existing owner/source checks.

The receipt records both the actual provider endpoint and the file's display
endpoint. These may differ when a CDN/public endpoint is configured. File proof
and alias lookups require the exact recorded bucket, display endpoint, key and
business/hash/length fields; they never equate a CDN with the provider endpoint.

A converted file retains its private verified source for actual source/stored
re-verification. The source is not an independently downloadable user file. File
tombstoning queues that exclusively owned source in the same transaction as the
stored object's deletion, retaining the existing active/shared-file guard.

## Candidate recovery

Failed or expired unadopted receipts enter `cleanup_pending` and queue only their
exact UUID-owned candidate keys. Retryable original client uploads are retained.
A committed receipt is never an orphan-cleanup candidate. The existing OSS
multipart cleanup worker also services byte-binding recovery; the existing
object deletion worker performs provider deletion.

A deletion acknowledgement alone is not a terminal absence proof. After all
exact deletion jobs complete, reconciliation performs provider HEAD outside the
database transaction and lists incomplete multipart copies under each exact
receipt-owned key. Only exact keys/upload IDs are aborted; prefix-only neighbors
are retained. Listing uses at most four pages of32 results per key. Denials,
malformed cursors and exhausted budgets remain pending. The existing required
bucket AbortMultipartUpload lifecycle of at most seven days remains a fallback.
This worker therefore needs ListMultipartUploads and AbortMultipartUpload for
its owned upload scope in addition to the existing object read/delete access;
this task does not alter remote permissions. A currently absent candidate with
completed bounded multipart cleanup becomes `discarded` and keeps an indexed
hourly recheck. A candidate still present is queued for deletion again.
Provider errors leave it pending. Claim and finish use the receipt row and exact
claim deadline, so late checks cannot overwrite a replacement claim. This makes
late provider completion recoverable without claiming that cancellation prevents
all late remote writes. It does not promise absence at every instant; workers
must run, and provider failures/dead jobs remain visible. Actual cloud late-copy
behavior is not inferred from protocol fixtures.

## Existing data

Old public IDs, objects, audit rows and references are not moved or deleted by
schema expansion. Old `source_hash_verified=true` alone is excluded from new
source-hash deduplication, including blueprint-original reuse; both use the same
matching committed source/stored byte-receipt predicate. Already
completed legacy reads/replays retain their existing identity. An existing
sealed receipt whose trust marker is cleared is re-established only by reading
its actual source/stored bytes and retaining the same file ID.
Aliases resolve only while the current file still matches its recorded uploader,
source, logical category, source/stored hashes and byte lengths. A later trusted
regeneration of a deleted physical key does not inherit the old upload alias.

Legacy objects lacking a sealed receipt still require scoped byte diagnosis and
an explicit alias-compatible promotion/compensation procedure. This checkpoint
does not automatically promote them, infer a missing converted source, or repair
production data. The readonly commands report candidates; those are not proof
of original byte identity or user intent. Original client-upload objects remain
private retry/audit inputs pending an authorized retention/cleanup procedure;
this change does not claim their physical storage was reclaimed.

## Installation and upgrade

Fresh installations include the typed receipts and reconciliation index.
Generation168 old installations inspect without changing data:

```sh
go run ./cmd/db-oss-byte-binding-upgrade
```

The default command uses a read-only repeatable-read transaction, fixed catalog/
public/temp search path, and count-only output. Missing schema/index is reported
as requiring expansion; incompatible same-name definitions are rejected. It does
not backfill proofs or change public IDs, file fields, balances or shared blobs.
The additive `file_endpoint` snapshot is independently inspected. An older typed
receipt table missing it requires explicit expansion; old rows receive an unknown
blank value and cannot supply a new location proof. No endpoint is inferred from
current settings or the file's potentially relocated metadata. Such early typed
rows remain in scoped legacy diagnosis until their actual source/stored bytes
and intended location can be established.

After an operator-owned backup and review, the explicit schema-only form is:

```sh
go run ./cmd/db-oss-byte-binding-upgrade --apply \
  --database '<exact database>' \
  --backup-reference '<existing backup identifier>' \
  --backup-metadata '<new private metadata file>'
```

The exclusive0600 metadata file is a pre-change diagnostic record, not a
substitute for the referenced real backup. Apply locks this expansion, checks
the exact target/generation, creates qualified public objects, validates their
structure and commits atomically. Repeat apply is idempotent; pg_temp shadows
cannot redirect it. Startup requires this expansion before new writers run.

Deploy compatible readers/alias handling, stop old upload-registration writers,
apply the additive expansion, then enable the new writers and recovery workers.
Do not mix old raw-key writers with the new completion protocol. Existing valid
incoming tickets can complete through the new path; old completed unbound objects
remain in the separately diagnosed legacy scope. No production execution has
been performed by this task.

## Rollback and validation boundaries

Keep additive receipt/alias/index data and adopted objects during rollback. Old
writers must not be reintroduced as a raw-key fallback. Keep cleanup/recovery
running or preserve pending receipts for a forward repair. Do not drop history or
bulk-delete shared objects to make checks pass.

Named regressions cover ordinary single upload, actual provider bytes and two
hashes, stable completion replay, incorrect declared hash rejection, exact owned
candidate compensation, expired lease fencing, converted-source retention and
file deletion, fresh/old/idempotent schema upgrade, schema-drift rejection, and
SDK2GiB multipart-range/encryption-header contracts. Protocol-provider tests do
not establish actual cloud versioning, SSE/KMS encryption or2GiB payload streaming.
The previously automatically blocked client-overwrite reproduction has not been
retried or replaced; its unverified layer remains explicit in the task report.


The optional maintenance journal also has additive typed reference-scope fields:
reviewed schema fingerprint, an operator pause assertion, individually paused
reference tables and actual final-executor fence tables. Explicit expansion of
an old journal adds only unknown blank/false/empty values and preserves existing
IDs, plan digests, tokens, timestamps, preimages and file/receipt identities.
Normal upload startup remains compatible with that old optional journal, while
data-tool apply requires the explicit expansion. Retain the additive columns
and private journal on rollback; a code rollback cannot reverse external object
deletions. See `audit-oss-byte-maintenance.md` for the required writer/DDL pause
and bounded actual lock scope.
