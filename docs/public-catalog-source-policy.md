# Public catalog sources

Global resource presentations, recipe/type/tag/template lists and details, assets, short links, public tag/type/recipe/template localized content and article resource references and tag/recipe-type comment targets use the same public source predicate in `internal/catalogvisibility`. An active import observation contributes to these responses only when its owning Mod is approved and the import is active with ready/partial status. Blueprint required-Mod summaries, Mod-based blueprint search matches and catalog asset revision lists omit withdrawn/unapproved source Mods; approved sources contribute these associations again. The blueprint remains available according to its own existing owner/review policy. Manual project observations likewise require an approved owning Mod and an active version/detail. Filtering happens before choosing the latest candidate or limiting results, so a newer private observation cannot displace an older public one.

Independent, published canonical definitions remain available even when an unrelated project observation becomes unavailable. Legacy manual identities without import/project observations retain their existing public behavior. Technical resource content administration retains its existing global_resource.view boundary. Project editing and revision preview endpoints keep their existing scoped permissions; being an editor does not make a global catalog response private.

Shared catalog loaders clear request-specific preview claims and use a `public-source:v1` cache prefix together with the existing dataset generation. Changes to publication must bump that generation in the same publication transaction. Direct database edits that bypass the supported publication paths are outside this invalidation contract. The regression fixture explicitly advances the generation when simulating direct owning-Mod state changes; a separate warm-cache assertion invokes the supported moderation transaction and verifies its generation change hides the source immediately on the next API request. This is not proof that a production operator's direct SQL change invalidates cache entries.

JEI category catalysts use the exported ingredient type and Minecraft resource identifier. Each published chemical or fluid resolves its own icon and name even if an item uses the same identifier. A supplied site ID cannot substitute for the offline game resource identifier.

Imported recipe and catalyst maps are treated as untrusted input. Derived links, names, source/version identifiers and image paths are removed before authoritative resolution and rebuilt only from public observations. An unresolved reference retains its raw identity, without a supplied presentation URL. Existing article resource references whose source becomes unavailable return an unavailable reference with no source metadata; they are not silently converted into new raw identifiers.

Registration and content preference updates distinguish primary content language, secondary content language and UI language. The primary content language can be any valid BCP-47 tag. The secondary language must be one of the eight supported human-editable content locales; supported aliases normalize consistently. UI language is stored separately. Administrative user details report the secondary content language rather than the UI language.

No schema migration or production data cleanup is required for these HTTP projection changes. Tests use synthetic data in actual isolated PostgreSQL databases, including pending/approved transitions, independent canonical fallback, private assets and short links, unresolved presentation fields and constant-query batched reads. They do not validate production cache topology, data quality or supplier translation quality.

Legacy canonical references can resolve through the existing list APIs before navigating to a public ID: `GET /api/v1/tags?canonicalId=...&registry=...` filters both identity fields exactly, and `GET /api/v1/recipe-types?canonicalId=...` filters the recipe-type identity exactly. These optional parameters preserve the existing response envelope, pagination, fuzzy `q` behavior and public-source policy. Unknown identities return an empty list. The recipe-type cache includes the exact filter. Clients must URL-encode values and navigate only after matching the returned canonical identity and registry.

A report moderation deletion keeps the last published project revision for audit and makes its catalog sources ineligible. A pending ordinary review resolves as a conflict against that moderation action. Automatic project revision publication also checks the decision under the project row lock. Reopening a report resets its workflow and pending evidence cleanup. A project restoration workflow is still pending. First-review rejection and the archived upstream status continue to use ordinary project review.

### Revision registry reads

Both revision registry routes check the same revision visibility before returning entries.
Use `GET /api/v1/export-revisions/{revisionId}/registry-entries?registries=items&summary=1&limit=80&offset=0` for a list. The default page contains at most 80 rows, the maximum is 100, and the response includes `total`, `limit` and `offset`. Set `locale` for translated display names. With both `items` and `blocks`, the list suppresses item rows already represented by block bindings in that revision. Request each registry separately when counting its snapshots.

`GET /api/v1/export-revisions/{revisionId}/registries/{registry}` returns the complete single registry ordered by canonical ID, including stored snapshot data and names, within 100 entries and a 2 MiB budget. Larger results return HTTP 422 with `registry_response_too_large`, a `Link` header and `details.pageURL`. Switch to that paginated endpoint and advance the offset by each returned page size. The summary projection omits entry data. The paged detail projection processes names separately. Use the individual entry detail when the paged summary lacks the fields needed by a client.

### Resource names and registries

For a resource such as `example:plate`, the identifier namespace is `example`. Catalog resource cards and picker results expose that value as `namespace`. Their `registry` field gives the known resource family, for example `items` for `minecraft.item`. Extension kinds use the registry from the visible source snapshot when present. Without an observed registry they leave this field empty. Tags retain their exact registry, such as `minecraft:item`, as part of their identity.

Both resource directories accept `?namespace=example` and `?kindCode=minecraft.item`. The revision-specific registry routes continue to use the stored exporter registry name. Anonymous single-resource and batch cards use approved names, with the same language fallback.

For a single icon, pass a resource public ID in `ref`, or specify the canonical identifier and its exact kind, for example `?ref=example:plate&kind=minecraft.item`. An untyped identifier matching multiple visible resources returns HTTP 409 with `resource_reference_ambiguous`. Batch hydration lists ambiguous positions in `unresolvedInputIndices`. A known family or exact kind distinguishes resources in a batch. For tags, supply the registry as well as the canonical ID.

The public recipe-type directory searches canonical identifiers and displayed titles in every available language. An approved manual name replaces the import title in its own language. Other import languages stay available, and pending or rejected names stay out of the directory and its search results.


### Manual recipe content

The manual editor publishes recipes through `publishCatalogRecipeTx`. The reviewed definition, selected template, bindings and candidates supply the canonical layout. Recipe cards and category pages display the approved localized recipe body in `note`; the preferred language wins, followed by the secondary and the recipe default. Imported recipe observations supply layouts where a canonical definition is absent.

The former global recipe editor last wrote `recipe_content_overrides` before commit `c63c22a` in August 2026. The current empty-database installation omits that development table. New corrections belong to the reviewed editor and the existing localization or import history.
