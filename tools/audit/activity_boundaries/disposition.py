#!/usr/bin/env python3
"""Merge reviewed CMP-003 boundary evidence into a diagnostic route snapshot.

No database access. No repair. Reachable calls and inferred defaults never
upgrade a route to tested acceptance; listed evidence has an explicit scope.
"""
import argparse
import collections
import hashlib
import json
import pathlib
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--inference', required=True, type=pathlib.Path)
parser.add_argument('--output', required=True, type=pathlib.Path)
parser.add_argument('--repo', required=True, type=pathlib.Path)
args = parser.parse_args()
report = json.loads(args.inference.read_text())
reviewed = {}

def add(handlers, commit, log, scope):
    for handler in handlers.split():
        reviewed[handler] = dict(commit=commit, evidence=log, scope=scope)

add('commentsForTarget commentItem projectChangelogs projectChangelogItem', '1fa99ab', 'logs/backend_revisions/pg-atomic-boundaries-final.log', 'Comment create/edit/delete, pending skip and independent approval; changelog create/edit. Comment idempotency, actual task/XP/currency recovery also in pg-comment-rewards-atomic.log; target and author scopes from those fixtures.')
add('reviewPendingComment', 'cc1ac92', 'logs/backend_revisions/pg-manual-review-and-queue-cursor.log', 'Pending comment approve/reject/restore, original author activity; queue pagination with105 visible+5 hidden parents. Earlier moderation authority/notification/watch tests also required.')
add('createMinecraftServer updateMinecraftServer ratingItem adjustAdminUserBalance', '6cd207e', 'logs/backend_revisions/pg-more-activity-boundaries-first.log', 'Server create/update using controlled real probe, rating create/update/delete, admin actual/no-op balance adjustment; precise activity failure, full business snapshots and retry.')
add('skins skinDetail myPlayerProfiles myPlayerProfile skinWardrobeItem setPlayerProfileTexture', 'd8c1f52', 'logs/backend_revisions/pg-skin-activity-corrected-constants.log', '9 write verbs across library/profile/wardrobe/equip; actual PNG and controlled OSS, shared approved blob remains intact; exact create/edit/delete3/3/2.')
add('communityPosts communityPostItem simpleProjects simpleProjectItem createMod createModpack modpackItem', '740ac02', 'logs/backend_revisions/race-project-community-activity.log', 'Four community and six simple project kinds create/edit; Mod create and modpack create/edit. Specific outbox failure and complete DB facts, independent review/publication. Mod normal route correction logged pg-mod-pack-activity-corrected-route.log.')
add('reviewContentRevision reviewModRevision', '7c85df1', 'logs/backend_revisions/pg-mod-blueprint-review-final.log', 'Shared manual-review successful resolution boundary verified by catalog decision; automatic Mod approval object annotation distinction verified. Other transitive review kinds still need their own original policy matrix; this does not certify all aggregates.')
add('submitModRevision updateBlueprint', '7c85df1', 'logs/backend_revisions/pg-mod-blueprint-review-final.log', 'Mod revision review-required/no-review; blueprint initial metadata/edit with independent approval. Outbox failure, full facts and exact original event identity.')
add('acceptCommunityPostAnswer selfSolveCommunityPost reviewMinecraftServer', '4d39641', 'logs/backend_revisions/pg-settlement-activity-first.log', 'Award/refund held bounty under existing policy, server approve/reject, financial ledger/notification/activity rollback and conflict replay. Existing money concurrency matrix retained.')
add('reviewCreatorClaim revokeCreatorClaim updateCreatorMembers reviewProjectEditorApplication', '36b495c', 'logs/backend_revisions/pg-governance-activity-first.log', 'Creator approve/reject/revoke and derived access, team membership, editor app approve/reject and assignment, complete audit/notification/facts rollback; independent ordinary reviewer.')
add('convertBlueprint retryBlueprint', '09cf6df', 'logs/backend_revisions/pg-blueprint-job-activity-first.log', 'Activity/job/NATS intent atomic; retry after3 actual controlled source GET503 Worker failures; recovered actual Worker completes, public artifact. All3 blueprint format neighbors retained.')
add('launcherCredential launcherSession followUser userBlock deleteUserOSSFile', '98810a7', 'logs/backend_revisions/pg-user-activity-first.log', 'Credential PUT/DELETE including absent-account branch and session revoke, follow, block/unblock, actual user file deletion/quota/outbox; exact activity fault rollback/retry. Does not cover unfollow or unrelated user config.')
add('createCreator updateCreator claimCreator purchaseShopItem useShopItem', 'fd4d188', 'logs/backend_access/claim-and-activity-atomicity-first.log', 'Backend_access verified creator create/edit/claim and shop purchase/use financial/consumption rollback; retain separate agent evidence and access-race-final.log.')

add('checkIn transferCurrency', '1bfb498', 'logs/backend_revisions/pg-economy-activity-first.log', 'Ordinary user check-in/transfer: precise activity outbox failure preserves balances/ledger/check-in rows, recovery exact1 activity, duplicate check-in conflict, FID001/TEST044 neighbors.')

add('createFavoriteCollection updateFavoriteCollection deleteFavoriteCollection patchFavoriteMembership setFavoriteMembership', '831a6a8', 'logs/backend_revisions/pg-favorite-activity-first.log', 'Collection create/edit/delete, membership replace and both delta add/remove: exact activity failure leaves quota/popularity/export-task/association facts unchanged, recovery exact create/edit/delete1/4/1; visibility/popularity/DB-error neighbors under race.')

add('claimReport takeoverReport resolveUnifiedReport reopenUnifiedReport', '5f392ac', 'logs/backend_revisions/pg-report-activity-first.log', 'Actual HTTP report creation through claim, privileged takeover, valid-with-hide/invalid-without-hide resolution, reopen; exact outbox failure rolls all assignment/moderation/review/notification facts back, recover once, duplicate reopen409; existing responsibility/idempotency/punitive-boundary neighbors.')

add('createAdminUser updateUserPermissions updateUserStatus', '48c98b2', 'logs/backend_revisions/pg-admin-account-isolated-9dd89ed.log', 'Admin identity/default role/manual permission/status audit and intent rollback/recovery; exact owned3 files over clean9dd89ed base, source SHA index preserved. Original admin authorization/SEC004/SEC021 neighbors under race.')

add('unfollowUser updateNotificationSettings', '6f44a3c', 'logs/backend_revisions/pg-profile-preferences-activity-first.log', 'Existing/no-op unfollow and partial notification preference updates: precise activity fault leaves all relationship/user/preference facts unchanged, recovery exact1 event per call, cross-user preferences isolated. Additional isolated9dd+owned files race includes financial refactor and original language/follow/block neighbors.')

add('commentPin commentReaction', 'e8d17f3', 'logs/backend_revisions/pg-comment-interaction-activity-isolated.log', 'Pin/unpin preserve explicitEdit and reaction add/remove preserve existingEdit/Delete; precise activity failure full comment/counter/heat/association snapshots unchanged, recovery exact1 intent, full original TEST038 matrix normal20.264s and race28.171s.')

existing_fact_handlers = {'contentLanguageSettings', 'createCatalogResource', 'catalogTags', 'catalogTagDetail', 'createCatalogRecipeType', 'catalogRecipeTypeDetail', 'createCatalogRecipe', 'catalogRecipeDetail', 'catalogRecipeTemplates', 'catalogRecipeTemplateDetail', 'updateCatalogEntityContent', 'ownedSkinContent', 'ownedBlueprintContent'}
counts = collections.Counter()
for route in report['routes']:
    handler = route['handler']
    if handler in reviewed:
        route['reviewed_disposition'] = 'ATOMIC_AT_LISTED_TESTED_SCOPES_REQUIRES_FINAL_FIXED_COMBINATION_REGRESSION'
        route['reviewed_evidence'] = reviewed[handler]
    elif handler in {'commentWatch', 'commentWatchItem'}:
        route['reviewed_disposition'] = 'EXPLICIT_SKIP_PRIVATE_USER_STATE_NO_NEW_REWARD_POLICY'
        route['remaining_condition'] = 'Production handler unconditionally skips request activity at entry; preserve private watch state policy instead of inventing activity or rewards.'
    elif handler in existing_fact_handlers:
        route['reviewed_disposition'] = 'EXISTING_ACTIVITY_FACT_IN_BUSINESS_TRANSACTION_PROGRESSION_POLICY_SEPARATE'
        route['remaining_condition'] = 'Verify specific HTTP branch and historical progression contract; do not create duplicate intent merely because immutable fact has no outbox row.'
    elif route['default_classification'].startswith('INTENTIONAL_'):
        route['reviewed_disposition'] = 'INTENTIONAL_HIGH_FREQUENCY_DEFAULT_EXCLUSION'
        route['remaining_condition'] = 'Check explicit annotations before concluding no activity; not product acceptance.'
    elif not route['default_activity_valid']:
        route['reviewed_disposition'] = 'NO_DEFAULT_EVENT_CHECK_EXPLICIT_ANNOTATION_DYNAMIC_DISPATCH'
        route['remaining_condition'] = 'No new rewards authorized by this observation; inspect concrete kind and branch annotations.'
    elif route['static_reachable_atomic_functions'] or route['static_reachable_activity_fact_functions']:
        route['reviewed_disposition'] = 'STATIC_ATOMIC_REACHABILITY_ONLY_BRANCH_UNVERIFIED'
        route['remaining_condition'] = 'Inspect every successful branch; actual fault and postcondition evidence needed.'
    else:
        route['reviewed_disposition'] = 'OPEN_BUSINESS_OR_TRANSIENT_BOUNDARY_REVIEW'
        route['remaining_condition'] = 'Classify actual business writes versus read/probe/presign semantics; existing middleware intent may still be attempted after response. Add exact transaction/fault/recovery evidence for reachable business mutations.'
    boundary = route.get('direct_boundary') or {}
    file = args.repo / boundary.get('file', '')
    if file.is_file():
        route['direct_source_sha256'] = hashlib.sha256(file.read_bytes()).hexdigest()
    counts[route['reviewed_disposition']] += 1
report.update({
    'version': 2,
    'source_backend_head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=args.repo, text=True).strip(),
    'source_tracked_dirty_files': subprocess.check_output(['git', 'diff', '--name-only'], cwd=args.repo, text=True).splitlines(),
    'counts_by_reviewed_disposition': dict(counts),
    'acceptance_limit': 'This is a route boundary review aid with evidence scopes, not a PASS/FAIL count, not262 defects, and not61 defect or full release completion. Shared-call reachability is never evidence of a particular successful branch.'
})
args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
print(json.dumps(dict(route_count=report['route_count'], dispositions=dict(counts)), ensure_ascii=False))
