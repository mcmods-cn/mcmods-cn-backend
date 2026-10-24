# GR-R02: abnormal localization projections and public readers

This remains a risk item from the 2026-10-24 V2 audit. Its original dynamic case directly inserted `pending`/`rejected` `content_localizations` for an already public Tag. That demonstrates a missing defensive reader boundary; it does not demonstrate that an ordinary product user can create the state. The normal write and abnormal database-injection cases are kept as separate tests and results.

The normal HTTP case creates a Tag, independently approves it, submits an ordinary user's Chinese localization, checks pending projection state and the independent review queue, rejects it, then submits and independently approves a new localization. In that path, proposals stay in revisions/change requests. The public projection has only approved rows. The existing catalog/localization publishers explicitly write `review_status='approved'`. This is bounded counterevidence for those tested writers, not a proof about every historical/external writer or database state.

Public catalog detail, directory names, name-based matches/counts, member previews, alias/presentation batches and Tag/recipe-type comment titles now require approved localization rows. Public resource search documents apply the same condition before publishing names to the index. The public content-resolution API already used the approved boundary. Independent review continues to use its scoped pending revision queue and review actions; no public detail endpoint is promoted into an unreviewed-content preview.

The abnormal-state test creates the public Tag normally and then explicitly inserts synthetic pending/rejected projection rows in its exclusively owned database. The regression asserts that anonymous Tag detail, directory, content resolution and comment-title responses omit those names and bodies; name search returns neither items nor total for them. The injected rows remain intact: this repair changes readers and does not erase audit/state evidence. A separate search-document regression excludes nonapproved resource names while preserving approved canonical/imported names. An additional real PostgreSQL + Typesense 29.0 test observes both synthetic legacy terms in a version-5 collection, invokes the actual business rebuild Worker, verifies committed version-6 state and alias switch, verifies approved name retention and zero matches for both nonapproved terms, confirms old collection deletion, preserves the abnormal business rows, and repeats the rebuild idempotently.

No production diagnosis or data rewrite has run. Existing nonapproved localization rows should be inspected read-only, with only counts/public IDs and review status in reports. They should not be automatically approved, deleted, or treated as proof of a normal-user exploit. Projection version 6 forces a new collection build and alias switch on upgrade from version 5, removing any historical unapproved resource terms already indexed. Until the rebuilt alias is ready, the existing SQL fallback applies; reserve space for both collections. Old workers stop syncing when they observe the higher version. Reverting application code does not safely restart an old worker: keep the projection pipeline stopped or deploy a corrected version 6/newer; do not lower persisted version markers. Serving an authoritative result remains subject to current source visibility. Historical third-party downloads cannot be recalled.

Normal write reachability not reproduced is not an excluded risk. The exact tested normal path, abnormal injected case, code commit and logs are recorded in `agent-results/gr-r02.json` in the portable task evidence. Final release acceptance and broader writer coverage remain independent of this reader hardening.

An operator can run this bounded read-only diagnosis on an authorized copy; it returns counts and public identities only:

```sql
begin transaction read only;
set local statement_timeout = '30s';
select subject_type,review_status,count(*) as rows
from content_localizations
where catalog_entity_id is not null and review_status<>'approved'
group by subject_type,review_status order by subject_type,review_status;
select distinct route.public_id,route.entity_type,localization.review_status
from content_localizations localization
join public_routes route on route.entity_type=localization.subject_type
  and route.internal_id=localization.subject_id
where localization.catalog_entity_id is not null
  and localization.review_status<>'approved'
order by route.public_id,route.entity_type,localization.review_status limit 1000;
rollback;
```

No repair is inferred from these candidates. The source of a nonapproved row and its intended review history require investigation before data changes.
