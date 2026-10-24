# FID-001 / FID-002 / CMP-005 identity and source-hash expansion

Deploy the additive database expansion before this backend version. On an owned
copy, first run `go run ./cmd/audit-identity-oss` for read-only schema/legacy-row
counts. Apply uses `--apply --database EXACT_DATABASE --backup-reference BACKUP_REFERENCE --backup-metadata NEW_PRIVATE_FILE`;
the reference identifies an existing operator backup and is not created or
verified by this command. The new 0600 metadata file records the pre-change schema state and counts, not the actual object bytes or a data backup. The command never adjusts balances or removes files.
Fresh installations receive the same columns through the normal schema installer.
All expansion statements are transactional and repeatable; old queue IDs and relations
stay unchanged. Each queue gets a globally reserved random public ID. Queue API
clients must use these string IDs and related users/files/projects' public IDs.
Old numeric queue URLs are rejected. Deploy the backend and queue UI together.

`oss_files.sha256` remains the hash of original source bytes. Conversion may store
a different PNG/WebP body; the source hash is verified before conversion. Every
new user upload is streamed and hashed before registration. Existing files start
with `source_hash_verified=false` and cannot participate in hash reuse. A clean
virus scan or a metadata SHA is not a byte-hash verification. Diagnose old rows
by scoped ID and verify original bytes on a copy before considering a future
explicit marker update. Converted legacy objects whose original source no longer
exists cannot be proven from converted bytes. Preserve shared references and audit
history; this migration intentionally leaves their markers false.

Rollback application code first. Retain the added columns and public IDs: dropping
them breaks queued links and destroys verification evidence. Reverting to the old
application also reintroduces the reported identity/hash issues; it is not a safe
security rollback. Production diagnosis/execution has not been performed.

Transfers accept an explicit `recipientPublicId`, mutually exclusive with legacy
`recipient`. Legacy username/email/public-ID lookups reject matches to multiple
accounts with `TRANSFER_RECIPIENT_AMBIGUOUS`; internal numeric keys are never lookup
values. Existing balances and transfer ledgers are not rewritten. To diagnose old
misdirected transfers, correlate original request evidence with both public and
internal identities and the immutable double-entry ledger. A ledger alone cannot
prove the sender's intended recipient; any compensation requires a reviewed,
scoped operation and separate production authorization.
