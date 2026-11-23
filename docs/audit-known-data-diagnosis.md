# Read-only diagnosis of historical data impact

`go run ./cmd/audit-known-data --since RFC3339 --until RFC3339 --limit 100`
uses a repeatable-read, read-only transaction and a 30-second statement budget.
It returns counts and bounded samples of public IDs. It does not output balances,
private JSON values, object keys, signed URLs, tokens or private titles. It does
not fetch any object-storage body. Run against an authorized copy that matches
the [current installation baseline](../README.md#数据库安装发布和恢复). Missing
required proof fields cause a query error and a nonzero exit. No production
diagnosis or repair was executed by this task.

The transfer section identifies actual ledger recipients whose internal numeric
key could have collided with a different account's numeric public ID or username.
It cannot infer the sender's original input, distinguish leading-zero strings from
original intent, or prove a transfer was wrong. Correlate retained original HTTP
request evidence and immutable ledger pairs. Never repair balances automatically;
a confirmed case needs an individually reviewed compensating ledger entry and
explicit production authorization. Preserve existing history.

Source-hash diagnosis counts unverified active files and their actual single-column
foreign-key references in the current schema. These are unknown hashes, not proven
bad files. Do not delete or rewrite shared references. For immutable ordinary source
objects, reverify bytes before setting any marker. For converted assets with a
missing original source, `stored_sha256` cannot prove the original source hash.
The companion `go run ./cmd/audit-identity-oss` checks the current schema and reports counts. Optionally pass `--database EXACT` to verify the database name. Source-hash markers require verification of the original bytes.

Private-skin rows and JSON sensitive-key matches are exposure candidates. JSON key
matching is deliberately diagnostic: legitimate business values, unknown extensions,
all original attachments, and PNG ancillary chunks need purpose-aware inspection on
a scoped copy. Existing downloaded copies cannot be recalled. The game-import fix
must be used to re-project a reviewed, sanitized package; do not bulk-delete manual
content, assets or immutable audit snapshots. A future repair must record exact
public-ID scope, dry-run counts and before/after hashes, preserve shared references,
be idempotent, and provide a restore/compensation plan before production execution.

The rejected-active-import section detects inconsistent state candidates. It does
not guess which previous approved import to restore. Choose the effective revision
from retained review history and current state-machine rules, then rebuild projection,
assets, events and caches together using a scoped reviewed repair. The application
now rejects an approved-to-rejected transition rather than changing only status.
