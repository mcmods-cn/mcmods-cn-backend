# Game import review scopes

The pending queue, import detail `canReview`, and decision action use the same
existing project-review policy. Site-wide `content.review`, `project.review`
and the existing administrative wildcard can review any project submission;
`project.review.<publicProjectId>` is restricted to that exact project and
cannot decide the reviewer's own submission. A project editor may read their
unpublished import but editor permission alone does not grant review.

The queue route authenticates first and its `canAccessAnyReviewQueue` gate
then evaluates the effective review scope, including explicit denies. Import
rows are filtered by current project ID and submitter rather than classified as
unconditionally global catalog edits. Unrelated global catalog review rows
retain their separate global-content policy. Cross-project or self-scoped rows
are omitted from both items and totals. Revoked users cannot retain access by
using a previously returned review URL.

This change aligns discovery with review authority already enforced by
`canReviewProjectSubmission`; it does not introduce a new review permission,
change global/scoped self-review policy, or grant authors a bypass.

`TestAuditGR004IndependentQueueReadDecisionRoleMatrixHTTPPostgresIntegration`
uses mounted HTTP and isolated PostgreSQL for anonymous, ordinary, read-only,
creator, content reviewer, project reviewer, exact project reviewer, unrelated
project reviewer, scoped self-reviewer, global self-reviewer, administrator and
revoked reviewer. Denied actions preserve task, source, projection and outbox
snapshots. The existing original `content.review` case remains covered.
