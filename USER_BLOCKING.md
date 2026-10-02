# User blocking

`user_blocks` is the authoritative, directional blacklist relation. A row
`(blocker_id, blocked_id)` means that the blocker no longer sees comments from
the blocked account.

## Interaction rules

- Creating a block is idempotent and removes existing follows in both
  directions in the same PostgreSQL transaction.
- Follow, block and unblock mutations acquire the same transaction-scoped
  advisory lock for the unordered user pair. Follow rechecks both directional
  blocks while holding it, so an in-flight follow cannot recreate a relation
  after a block has committed. The lock also covers the follow notification
  outbox write; failures roll back the relationship and its event together.
- A follow or direct message is rejected while either user has blocked the
  other. Existing direct-message history remains readable.
- Comment list, reply, thread, and watch responses filter authors blocked by
  the current viewer in SQL; filtered records are also excluded from totals.
- A blocked reply does not create a reply/watch notification for the blocker.
- A user blocked by a project owner cannot comment on that project or its Mod
  resource-version pages. Project ownership includes the direct creator and
  users resolved as verified developers for the project (or explicitly granted equivalent access by an administrator).
- Tutorial and discussion authors receive the same protection. BUG/feature and
  news authors deliberately do not, matching the community moderation model.
- Blueprint, skin, and player-profile owners are also treated as owners of
  their comment targets.

## API

- `GET /api/v1/users/me/blocks?page=1&pageSize=24`
- `PUT /api/v1/users/{publicUserId}/block`
- `DELETE /api/v1/users/{publicUserId}/block`

All three routes require authentication. The blacklist list is never exposed
through another user's public profile. Public profiles only return whether the
current viewer has blocked that profile; they do not disclose whether the
profile owner has blocked the viewer.

The paged list consumes and closes its database rows before loading storage
configuration or resolving avatar URLs. This allows the request to complete
with a one-connection pool and prevents avatar lookups from waiting for the
connection retained by the list itself.

## Database and upgrade

The generation 168 empty-database schema contains the directional table,
reverse lookup index, and unambiguous comment-route parameters. Other existing
generations are rejected; development reset is not a data-preserving upgrade.
The explicit generation-168 comment popularity function/binding repair and its
backup/restore procedure are documented in `docs/database-function-repair.md`.
