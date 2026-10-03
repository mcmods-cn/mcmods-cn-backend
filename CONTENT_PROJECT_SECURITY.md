# Project parent references and stored icons

Simple-project add-on references selected by public ID must identify an approved,
published Mod, modpack, or simple project. Creating or revising a project with
an unpublished selected parent returns HTTP 400. Free-text unresolved references
remain supported; their resolution waits for publication of the target.

Detail responses, catalog cards, parent facets and parent filters omit resolved
references whose target is no longer published. This also protects associations
accepted by older code. No records are deleted and no schema migration is
needed. Editors can replace an unavailable association with a published target
or an unresolved identifier.

Modpack and simple-project creation and revision validate internal storage icon
URLs inside the write transaction. The referenced file must be active, scanned
clean or trusted generated, and a supported raster image; the editor must own
the file or it must have an authorized public binding. A stored URL alone grants
no access. Accepted internal URLs are normalized to stable URLs without prior
signature query parameters. Invalid image references return HTTP 400; database
validation failures return HTTP 503. External HTTP image URLs retain the existing
behavior.

Imported modpack and simple-project icons and author avatars are prepared before
the business transaction acquires its connection. Slug allocation, parent
validation and final owned, active image checks remain inside that transaction.
This avoids holding a transaction while the image helper reads settings or
registers files through the pool. Icon preparation failures retain the existing
HTTP 502 response for modpacks and HTTP 400 response for simple projects.

The shared stored-image resolver applies the same authorization boundary before
generating signed URLs for existing project records. Unavailable historical
images fall back to an empty URL. A file deleted while a revision awaits review
can still leave an approved snapshot without a usable icon; this change protects
access but does not add a separate review-time repair workflow.

These changes require coordinated backend deployment with the shared storage
authorization helper. They introduce no API field or database schema changes.
PostgreSQL and synthetic local test data cover unpublished parent read/write
paths and private, unscanned, non-image, missing and owned project icon references.
No real user files or external storage credentials are used by those tests.

Mod content versions, templates, sections, cards, advancement graphs, resource
details and resource history also check the parent Mod's publication state.
Active child content does not make an unapproved parent public. Visitors and
submitters without explicit content authority receive HTTP 404. Project
editors and content reviewers retain their existing content preview access.
Global resource version decorations list only versions of approved Mods.
Server mod associations retain raw identifiers and provenance when a linked
Mod becomes unapproved, while omitting its resolved name, site ID and icon.

Export data summaries apply the same parent boundary, with project reviewers
as the preview role. Individual export revision APIs require both an active
version and an approved parent for public access; project editors and project
reviewers can preview other revisions with private, no-store responses. Their
existing HTTP 403 review rejection is retained. Existing public cache lifetimes
and previously issued signed URL lifetimes are unchanged; removal from public
visibility does not revoke already delivered copies or existing cached replies.
The read checks reuse the original identity query and require no migration.

Cross-project resource references (including recipes, loot tables, compatible
enchantments and blueprint materials) resolve published source projects. An
active import revision alone does not publish its parent Mod. Private source
previews require the current request's existing edit or review permission and
a source in the same Mod as the selected import revision. A preferred revision
ID alone grants no access. The manual resource fallback uses the actual Mods
schema, without a nonexistent status column.

Import creation returns HTTP 202 only after the committed task can be read and
its response fields decoded. A read-back failure returns HTTP 500; the committed
task and its queue event remain durable. Retrying the same package, target
version and overwrite option reuses the existing queued task rather than
creating another task or queue event. This change needs no migration.

Resource reference decorations are rebuilt from authorized, resolved resources.
Imported `resourceSources` values do not supply links, names or asset metadata
for unresolved references. Their raw canonical identifiers remain available.
Sequential decorations preserve results already resolved during the request;
the serialized response field names are unchanged.

Manual resource fallback selects details only from versions of the resource's
bound project. A later observation of the same canonical resource in another
project cannot provide its name or version through the manual fallback. Import
observations continue through the separately authorized import resolver.

Concurrent recipe parsing preserves the original task cancellation cause and
does not consume decoded results arriving after cancellation. Workers drain
before returning; parser success is not reported for a cancelled task.

Imported PNG media require PNG headers and a complete successful image decode
before being registered or uploaded. Truncated images and other formats renamed
to `.png` fail the import. The existing 100-million-pixel limit remains in place;
full image validation runs one image at a time across workers to bound concurrent
pixel allocations. Waiting for that slot respects task cancellation. Storage
upload concurrency and original image bytes are unchanged.
