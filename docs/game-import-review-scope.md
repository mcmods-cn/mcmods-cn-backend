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

A configured `project.review.*` is an existing scoped wildcard, accepted by the
permission-node and manual user-grant APIs. It follows the normal effective
rule resolver, and remains subject to exact project denies and scoped
self-review rejection. Queue filtering sends a separate wildcard boolean and
only real, effectively denied project IDs to SQL; it does not enumerate every
project or interpret permission syntax as a public project ID. Pending project
reads use the same effective scope. All facet counts are computed after scope
filtering, including empty queues.

`TestAuditGR004RealConfiguredWildcardScopePreservesDenyAndSelfHTTPPostgresIntegration`
creates and grants that permission through the actual mounted administrator
APIs. It verifies allowed/denied project reads, queue items/total/facets,
self-review refusal and independent approval. Its opt-in browser observation
mode releases the DDL lock while serving and retains a clean source tuple;
normal test runs do not open a live observation window. No reviewer grant is
added to deployed accounts by this change.

## 重复批准与事务

同一活动修订重试批准时返回现有发布结果。只有首次状态转换才更新激活时间、投影、审核事件和目录代数。已公开修订仍拒绝改判驳回；撤下须走带投影清理的归档流程。两条导入 Worker 使用 `finishStagedCatalogRevisionsTx` 核对任务、执行令牌和完整修订集合。免审发布与人工批准调用同一 `publishCatalogRevisionsTx`，按资料版本、项目、namespace 和 source_kind 替代已有活动修订。可观察的源数据、权限和状态条件分别留在各自入口，审核事件或任务结果随发布事务提交。
