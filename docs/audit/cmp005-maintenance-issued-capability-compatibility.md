# Seven-day maintenance and issued write deadlines

The terminal-file raw-retirement expansion adds `oss_upload_capabilities`.
Its `object_key` records an SDK-issued authorization, rather than a business
asset reference. The bounded maintenance inventory excludes this exact control
table only after the existing full byte-binding/raw-retirement schema guard
passes and its ten reviewed columns are present without extra columns. An
unreviewed extension fails closed; it cannot hide a new text/JSON reference.
Other reference tables, explicit operator write/DDL quiescence, the sixteen
physical-table lock limit and the existing bounded reference scan remain in
force. `onlineRaceSafe` stays false.

`cleanup_raw` additionally reads the exact bucket, provider endpoint and raw
key's real maximum SDK deadline. A separate new database-clock statement must
establish that this deadline plus one second has passed. A seven-day receipt
cutoff alone cannot establish this fact. Missing historical capability records
return `RAW_CAPABILITY_UNKNOWN_HOLD`; a still-live deadline returns
`RAW_CAPABILITY_LIVE_HOLD`. The normal preflight holds do not create a journal,
change a capability, change quota/file/receipt rows or touch object bytes.

The final executor repeats the deadline check after acquiring the original-key
mutex and its schema, reviewed table, action, receipt and file locks. Issuers
use that same original-key mutex and retain the maximum deadline. An existing
cleanup journal also prevents a later authorization from being issued for its
retired raw identity. The final check runs after the reference scan, before
the bounded provider DELETE/absence confirmation. A final eligibility loss
retains its named HOLD result; the caller records its existing failed intent
for inspection rather than pretending that physical cleanup succeeded.

There is no inferred backfill, capability deletion, production execution or
automatic schema change. Apply already requires the explicit byte-binding
expansion. Deploy that expansion and all deadline-recording issuers before
using the updated maintenance binary. Keep old issuers stopped during scoped
maintenance; a mixed writer that does not record grants cannot establish the
required proof. Unknown legacy grants remain held even after byte promotion.
Use the existing read-only diagnosis and an explicitly reviewed disposition;
do not manufacture a past deadline from file age or receipt age.

Excluding the reviewed control table changes the reference-schema fingerprint.
Regenerate and review a dry-run plan after the expansion. Existing journals,
backups and completed actions remain unchanged. A changed plan cannot reuse an
old action ID to defeat its plan hash; inspect that retained history separately
and review a new action only for genuinely outstanding work.

The regression fixtures request `expiresMinutes=1` through the already supported
API. They verify the actual SDK signature and retained deadline, perform one
initial PUT to a previously absent key, and wait naturally for that deadline
plus the guard interval. Synthetic old-storage conversion setup separately
signs and records a real one-minute grant for its old converted alias before
seeding that previously absent stored object. This is declared legacy setup,
not evidence of normal HTTP completion or historical authorization. The owned
receipt timestamp is adjusted only to exercise the existing seven-day retention
condition; no authorization deadline, database clock or object byte is changed.
All original promotion, raw-removal, absence, alias, compensation, byte and
identity assertions remain. The default 600-second and resumed 60-minute
physical-expiry proofs retain their own source pins and budgets; these one-minute
neighbors do not replace them.

The schema and default upload duration do not change in this compatibility fix.
Rollback to the older maintenance binary reintroduces its control-record false
reference and does not provide a safe bypass for a live/unknown grant. Retain
the new binary for maintenance or leave the operation held. Existing backups,
private sealed compensation bytes and journal history must be retained under
the reviewed plan's original recovery policy.
