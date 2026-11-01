# GR-Q03 / GR-Q04: queued import execution policy

The current task's explicit product decisions are: cancel when the queued creator's current account or project edit authority is invalid; also cancel an originally automatic job that loses its exemption from review. An originally review-required job needs current edit authority and may finish awaiting an independent review. An archived target cancels the attempt and requires a new upload. Cancellation preserves the job, reason and audit; it compensates only this attempt's staged revisions/assets. It does not roll back a previously approved import or rewrite audit history.

The same archive rule applies if the Worker finished and the target was then archived before independent review. A review request takes the common advisory lock before the target row, cancels the ready job's unpublished remainder and records one `execution_cancelled` policy audit, then returns 409. Unpublished revision rows become `rejected` so they cannot remain actionable in the review queue; this is explicitly a policy cancellation, not a fabricated human `review_rejected` decision. Already active revisions remain unchanged. Finalized source/asset evidence remains private under existing retention and access rules. Restoring the target cannot revive or retry this cancelled attempt. Reviewers and actual Workers use the same advisory → target row → revision ordering to avoid deadlocks.

`requested_publication_mode` records `review` or `automatic` at enqueue. It grants no permission. ZIP and embedded JSON workers re-resolve current account, effective RBAC including denies/expiry/project scope, and active target before execution and inside the publication transaction. Publication takes the same advisory-then-version-row lock as independent import review. A delayed delivery cannot claim a terminal cancellation; a delayed attempt cannot commit using an obsolete `run_token`. Source file ownership and job creator do not change on retry.

Existing generation-168 jobs receive `legacy_unknown`. Their past authorization cannot be inferred reliably. The DDL does not change their status, source or publication. When an active legacy job is next executed, it cancels with `queue_mode_unknown_reupload_required` and an audit entry. Already completed jobs remain historical. Re-upload is required for execution; the old task's retry endpoint and deduplicated source submission reject policy cancellation. No new outbox publication/retry intent is produced by cancellation.

## Read-only diagnosis and explicit upgrade

Use an isolated copy first. Supply the repository's normal `DATABASE_URL` without printing it. The default command performs only SELECTs and outputs the database name, generation, column presence, count and at most 1,000 task IDs of active legacy candidates. It never emits source paths, JSON, credentials or private names.

```sh
go run ./cmd/db-catalog-import-policy-upgrade
# After a real backup has been verified, on the intended copy:
go run ./cmd/db-catalog-import-policy-upgrade --apply \
  --database EXACT_DATABASE_NAME --backup-reference VERIFIED_BACKUP_REFERENCE
```

The backup reference is an operator assertion; this tool does not create a data backup. Apply requires a generation-168 database, an exact database name and a nonempty backup reference. The additive DDL runs under the shared migration advisory lock in one transaction. Repeating it preserves explicit modes, rows and keys. Startup checks the extension, text type, NOT NULL, review default and validated domain constraint; an old database without the extension receives an actionable error rather than a later request-time 500. Fresh installation includes the extension.

Apply fixes its transaction-local lookup order to `pg_catalog,public,pg_temp`, matching the public schema used by diagnosis and validation. A connection configured with another schema first cannot redirect DDL to an unrelated table; the caller's schema contents are preserved. The actual CLI subprocess tests cover both an already ready public schema and an old public schema with such a shadow table, read-only diagnosis, target/backup refusals, preserved legacy rows and repeated application.

Stop old enqueue endpoints and old import workers before upgrading; do not run mixed old/new workers. Back up, diagnose and review affected task IDs, run all required generation-168 extensions (including the separate identity/OSS and notification tools), then start the new backend and worker before reopening enqueue. The exporter payload is unchanged by this policy extension. Existing upload/project/hash/Minecraft/loader checks remain in force.

## Rollback and data preservation

Keep the additive column and cancellation audit during rollback. Reverting only application code would resume workers that lack current-policy checks; keep the import pipeline disabled until all queued/in-flight tasks have a reviewed disposition. Do not silently rewrite `legacy_unknown` to automatic/review, requeue policy cancellations, drop sources or edit balances. A cancelled attempt requires a new upload under the restored current permissions/active target. Restoring an archived target does not revive the old attempt. This tool has not been executed against production.

The integration tests separately exercise empty-database installation, the old column boundary with preserved legacy rows, repeat application, current-mode validity, pool sizes 2/12, cancellation, deadline, recovery and obsolete lease attempts. Controlled OSS/direct business Worker tests are not real NATS/browser end-to-end evidence.
