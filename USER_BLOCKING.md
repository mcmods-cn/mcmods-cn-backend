# User blocking

`user_blocks` is the authoritative, directional blacklist relation. A row
`(blocker_id, blocked_id)` means that the blocker no longer sees comments from
the blocked account.

## Interaction rules

- Creating a block is idempotent and removes existing follows in both
  directions in the same PostgreSQL transaction.
- A follow or direct message is rejected while either user has blocked the
  other. Existing direct-message history remains readable.
- Comment list, reply, thread, and watch responses filter authors blocked by
  the current viewer in SQL; filtered records are also excluded from totals.
- A blocked reply does not create a reply/watch notification for the blocker.
- A user blocked by a project owner cannot comment on that project or its Mod
  resource-version pages. Project ownership includes the direct creator and
  users bound to `project_owner.<projectID>`.
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

## Database and upgrade

Schema generation 77 creates the directional table and reverse lookup index.
The same upgrade also repairs two pre-existing PostgreSQL variable/column
ambiguities in the comment popularity trigger so a normal Mod comment can be
inserted when several project routes exist.
