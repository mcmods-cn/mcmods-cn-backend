# OSS byte-binding schema foundation (CMP-005)

This additive receipt schema and explicit upgrade command are a preparatory
checkpoint. The upload completion pipeline does not yet write these receipts,
seal client-uploaded objects, resolve aliases, or enforce their use for dedup.
Do not treat this migration as completion of CMP-005 or deploy it as its fix.
No production diagnosis, upgrade, object move or data repair has been executed.

The receipt preserves the original upload identity, declared source hash and
size, server-owned source/final keys, independently verified source/stored
hashes and sizes, file identity, attempt token and lease. A committed receipt
must satisfy the typed shape and unique upload identity. These constraints
cannot prove that application code read the bytes; that proof and lifecycle
are still required in the completion pipeline.

Fresh installations include the same schema. Generation168 old installations
can inspect it without changing data:

```sh
go run ./cmd/db-oss-byte-binding-upgrade
```

The default command uses a read-only repeatable-read transaction, a fixed
catalog/public/temp search path and count-only output. Existing
`source_hash_verified=true` rows remain unchanged and are counted as lacking a
binding. It does not infer a sealed-object proof, backfill a receipt, change a
public ID or move a shared object.

After a real operator-owned backup and review, the explicit schema-only form
is:

```sh
go run ./cmd/db-oss-byte-binding-upgrade --apply \
  --database '<exact database>' \
  --backup-reference '<existing backup identifier>' \
  --backup-metadata '<new private metadata file>'
```

The metadata file is an exclusive0600 pre-change diagnostic record, not a
substitute for the referenced database backup. Apply locks this expansion,
checks the exact target and generation, creates qualified public objects,
validates the proof function, columns, foreign key and committed identity
index, and commits atomically. Repeating apply is idempotent. Incompatible
same-name constraints/functions/indexes are rejected. A pg_temp table cannot
redirect the expansion.

A rollback of this checkpoint can leave the unused additive schema in place.
Once a later runtime starts adopting sealed keys, preserve receipts and adopted
objects; old writers must be stopped before enabling the new pipeline and must
not be reintroduced as a fallback. Alias-compatible reads, bounded candidate
cleanup with lease fencing, conversion from the sealed source, quota/blueprint/
report associations, ordinary completion replay and authorized legacy promotion
are still unimplemented. Their detailed design and validation boundaries are in
the task's `agent-results/cmp005-byte-binding-design.md`.

The isolated PostgreSQL test covers fresh install, old-schema expansion,
repeat apply, exact-target/backup checks, a temp shadow, unchanged legacy file
identity, exact23514 rejection of an unverified commit, and rejection of fake
CHECK/function/index/nullable identity definitions. It performs no provider
operation and does not repeat the dynamically blocked overwrite reproduction.
