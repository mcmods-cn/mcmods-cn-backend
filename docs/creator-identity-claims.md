# Author identity claims

An account may submit a claim for an approved personal author identity. Team identities are not claimable. A claim starts pending even when the claimant is an administrator or content review is configured for automatic approval.

The `creator.claim.review` permission allows reviewing claims on the existing administration endpoint. The reviewer must be a different account from the claimant. `admin.*` does not bypass this identity check: a claimant's request receives HTTP 403 with `CREATOR_CLAIM_INDEPENDENT_REVIEW_REQUIRED`, and the pending claim and derived access remain unchanged. This requirement concerns identity claims; it does not change the existing project or content review permission policies.

Independent approval closes competing pending claims and enables only the existing verified author/project and author/team relations represented by `effective_project_access`. Permission version refresh follows the committed update. Database failures while loading a claim return HTTP 500; missing claims return 404, and resolved or incompatible claims return 409.

Permission mutation auditing and the distinction between rollback and a committed version-refresh failure are described in [authentication and permission versions](../AUTH_AND_PERMISSION_VERSIONING.md).

Creator details return a storage error when approved/current-actor claim state or the claimed user cannot be read, rather than reporting an unclaimed identity. Missing claim rows remain valid unclaimed state. Image-signing failures are reported explicitly. Creator-role list reads reject malformed stored translation shapes and cursor failures; an empty valid translation map remains valid. Directory avatar authorization uses one batch for the page after its result cursor has closed.
