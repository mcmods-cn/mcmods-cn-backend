# CMP-003: durable activity transaction boundaries

Business mutations must persist the activity intent in the transaction which commits their successful state. A post-response best-effort attempt cannot reconstruct an intent lost before an idempotent retry. `enqueueRequestActivityTx` uses the same annotations, action/object inference, identity, Markdown deltas and exclusion rules as the legacy recorder. It persists only durable actions and marks the request to suppress a duplicate post-response event. Call it after the final annotation and before commit. Direct domain/Worker calls without HTTP annotations remain unchanged. Credential deletion and other successful no-op events retain their existing activity semantics; no reward policy is changed here.

Implemented and individually fault-tested families as of the commits below:

| Commit | Business boundary | Real verification |
| --- | --- | --- |
| c37392c, 1fa99ab | comment create/approve/edit/delete, changelog create/edit | HTTP + PostgreSQL + actual activity/progression consumers; outage rollback, restart, idempotent/concurrent replay, exact task/XP/currency/ledger |
| 6cd207e | server create/edit, rating create/edit/delete, actual/no-op admin balance adjustment | HTTP + PostgreSQL, controlled server probe; exact outbox failure and full business snapshots |
| d8c1f52 | skin/profile CRUD, wardrobe add/remove, profile texture equip | HTTP + PostgreSQL + controlled OSS with actual PNG, legitimate shared blob references |
| 740ac02 | four community create/edit, six simple project create/edit, Mod create, modpack create/edit | HTTP + PostgreSQL, independent review and publication, precise outbox failure and association snapshots |
| cc1ac92 | successful manual content review decisions | HTTP + PostgreSQL, decision/audit/notification/publication/activity rollback, duplicate409 |
| 7c85df1 | Mod revision submit, blueprint metadata | HTTP + PostgreSQL, review-required/no-review, initial blueprint/edit and actual source NBT |
| 4d39641 | question answer award/self-solve refund, independent server review | HTTP + PostgreSQL, real money/ledger/held state, notification intent and conflict replay |
| 36b495c | creator claim approve/reject/revoke, team members, editor application approve/reject | HTTP + PostgreSQL, permissions/audit/notifications and recovery |
| 09cf6df | blueprint conversion and retry | HTTP + PostgreSQL + actual business Worker with controlled OSS, precise activity failure rolls back job+NATS intent, recovered artifact becomes readable |
| 98810a7 | launcher credential/session, follow, block/unblock, user file deletion | HTTP + PostgreSQL, complete session/relationship/file/quota/outbox rollback; real controlled OSS |
| 1bfb498 | check-in and transfer | HTTP + PostgreSQL, exact balance/ledger/check-in rollback/recovery, FID-001 recipient boundary and economy concurrency neighbors |
| 831a6a8 | favorite collection CRUD, membership replace/add/remove | HTTP + PostgreSQL, quota/popularity/association facts, exact activity1/4/1 and error/visibility/popularity neighbors |
| 5f392ac | report claim/takeover/resolve/reopen | Real HTTP report creation and full workflow, exact failure rolls assignment/moderation/review/notification facts back; valid hiding/invalid preservation, idempotency and owner policy neighbors |
| 48c98b2 | admin account creation, manual authorization, status | Exact activity fault rolls identity/default role/manual grant/status/audit back; actual PG and isolated9dd+owned files race with original authorization/SEC004/SEC021 neighbors |
| 6f44a3c | unfollow and notification preferences | Actual/no-op unfollow, partial preference update and user isolation; PG fault recovery and isolated race |
| e8d17f3 | comment pin/unpin and reaction add/remove | Precise activity fault preserves full thread/counter/heat facts; recovery and whole original TEST038 matrix, normal20.264s and race28.171s. Pin verbs remain Edit; private watch retains explicit skip. |

These rows establish those tested boundaries. They do not establish every mutation route, every asynchronous consumer, final frontend behavior, or a frozen three-repository release. Additional commits may follow. The external audit result ledger records the exact commands, first failures, corrected fixtures, race results and remaining conditions. Creator create/edit/claim and shop purchase/use are handled separately in fd4d188; this document does not substitute for its test evidence.

Catalog and owned skin/blueprint localized-content edits already insert an immutable activity fact in their transaction and skip middleware. Changing their historical progression policy is a separate question from losing a post-response outbox intent. User content-language settings also already insert their immutable activity fact and skip middleware; original STYLE003 fault tests preserve that boundary. Editor application creation/assignment revocation have no default activity object under the current contract; this fix does not invent reward events for those routes.

## Read-only route diagnostics

The AST inventory is a branch-review aid, never an acceptance count. Server and Worker methods with identical names are disambiguated. The optional diagnostic test uses the actual production middleware inference and touches no database:

```sh
go run ./tools/audit/activity_boundaries -root . > /tmp/activity-boundaries.json
MCMODS_ACTIVITY_BOUNDARY_INVENTORY_INPUT=/tmp/activity-boundaries.json \
MCMODS_ACTIVITY_BOUNDARY_INVENTORY_OUTPUT=/tmp/activity-route-inference.json \
go test -tags=audittools ./internal/httpapi -run '^TestAuditToolActivityBoundaryInventory$' -count=1 -v
```

A route with a reachable atomic helper can still have uncovered success branches. A route without a default event may use explicit annotations or dynamically dispatch by project kind. Read/probe/presign routes are not automatically business mutations just because the historic HTTP inference marks them durable. The generated report deliberately gives no route a PASS.

## Historical data and production safety

`cmd/diagnose-activity-intents` performs a bounded, read-only Repeatable Read scan of a caller-selected interval and optional exact public author. It separates absent facts/intents from queued intents, excludes bodies and credentials, and has no apply mode. Candidates require correlation with immutable business evidence and ledger provenance. Do not manufacture historical activities, repeat financial rewards, or modify existing currency ledger rows. An authorized compensation would need its own idempotency key, bounded dry run/count reconciliation and reversal explanation. No production diagnosis or compensation has been executed by this task.

There is no database migration for these boundary changes. Existing intent consumers must remain enabled during deployment; pending intents retain their schema. Rolling back application code restores the historical post-response failure window, so keep this limit visible during rollback planning.

The legacy user-block regression fixture originally attempted `Migrate` against a populated shared `public` schema and stopped at the explicit identity/OSS upgrade gate. Commit 4a8dd4c uses the existing owned isolated empty-install HTTP fixture, retaining every original assertion. The first setup failure and the complete passing race rerun are retained separately; no shared/production migration gate was bypassed.

Commit784803d moves recipient resolution and transactional currency balance/ledger helpers, unchanged, into economy_ledger_store.go. The production handler remains1392 lines under the unchanged1500-line module gate. No source assertion or SQL was removed; the complete FID-001/TEST044/check-in/transfer and profile/language neighbor race regression passed. This pure module split is separate from the activity changes.

### Progression, role shifts and profile settings

Task create/edit/delete and level configuration now commit their existing durable activity with the definition/configuration and recalculation job. Manual role-track upgrades/downgrades enqueue inside the already locked user/track transaction, including the existing successful no-op response. Profile configuration and profile preference changes use the same boundary; the first actual user-profile revision retains its pre-existing Create annotation, later revisions retain Edit, and an empty preference request keeps its existing inferred Edit event. Authorization, reward rules, public-card validation and security-version refresh policy are unchanged.

`TestAuditFixCMP003ProgressionAndProfileActivitySharesBusinessTransaction` uses real HTTP and PostgreSQL, a precise activity-outbox constraint, full business snapshots, recovery and exactly one intent per successful request. It covers eight routes and eleven success branches, including no-op role shifts, first/later profile revisions and empty settings. `logs/backend_revisions/pg-progression-profile-original-create-annotation.log` passes (1.264s). Earlier logs preserve two invalid synthetic inputs correctly rejected with 400 and a mistaken expected Edit for the existing first-profile Create annotation; corrected tests preserve the product contract. Original role-track lock-order concurrency and the unchanged module-size gate also pass in the earlier mixed log, whose overall result remains FAIL because of its separate fixture setup error.

The REM-Q02 characterization is separate from repair. The real application endpoint accepts global `project.editor.review` (and the existing administrator wildcard) for self-approval. Content reviewer and per-project reviewer permissions do not satisfy that endpoint's global gate. `docs/project-reviews.md` describes content revisions using `canReviewProjectSubmission`; this is not a product decision for editor applications. The seven-role characterization passes and the item stays NEEDS_DECISION.

### AI creation, cached results and publisher retry

AI task creation and community translation enqueue now commit their existing activity with the task, quota reservation and NATS envelope. An accepted request that reuses the same task, or returns a cached translation, does not create a second business Create event or reward. Publisher retry keeps the same task/event and the existing transactional security app-log; it also does not invent a new Create or Edit reward. Request access logging remains separate. This aligns retry compatibility with the existing idempotent comment-creation rule; permissions, source visibility and supplier token accounting remain in force.

`TestAuditFixCMP003AITaskActivityAndDeliveryCreationIdempotencyIntegration` proves precise activity failure rolls back task/outbox creation, retries reuse the same task/envelope, an injected security-audit failure rolls back publisher retry, the recovered retry retains one audit, and the actual activity consumer awards exactly 7 XP and 2 diamonds once. `TestAuditFixCMP003CommunityTranslationActivitySharesQuotaAndTaskTransaction` covers exact failure, one quota-backed task/envelope, duplicate reuse and a typed cached-result fixture. That cached fixture is not provider execution evidence. The neighboring `TestTEST043ContentAIWorkerFailureSuccessAndDuplicateDeliveryIntegration` separately exercises the actual Worker with a controlled TLS provider, failure/recovery and duplicate delivery. The whole selected matrix passes in `logs/backend_revisions/pg-ai-and-community-activity-final.log` (5.234s).
