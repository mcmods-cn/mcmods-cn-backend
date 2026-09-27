package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const schemaGeneration = 167

// Migrate installs one coherent development schema. Generation 167 is the
// current pre-production baseline. Older development databases are
// intentionally reset instead of upgraded or backfilled.
// Generation 167 registers catalog-import derived objects before provider
// upload and gives failure, stale-run recovery, and retry a durable
// compensation lineage.
// Generation 166 gives project-statistics and comment-heat refresh queues an
// explicit pending/processing/failed lifecycle with bounded retry budgets.
// Generation 165 removes the unused sticker catalog version state after the
// client adopted bounded TTL caching and explicit mutation invalidation.
// Generation 164 aligns project-automation and recipe-binding status
// dictionaries with their writers and makes project update revision IDs
// numeric foreign keys.
// Generation 163 removes the unused favorite-export report_snapshot column
// and adds exclusive artifact ownership plus bounded orphan-recovery indexes.
// Generation 162 adds date-scoped token usage and in-flight reservations to
// seed-crawler translation tasks so concurrent workers share one hard daily
// AI budget.
// Generation 74 established
// numeric internal keys, globally unique public IDs, revocable authentication
// sessions, version-scoped mod-content layout identities and similar-resource
// groups, and generalized
// unresolved references together with the Minecraft server catalog, review,
// proof, dependency, status-history, and data-driven resource entry subtype
// models whose configurable fields also map imported compatibility and
// resource-specific encyclopedia data,
// including generic collected/uncollected resource references and normalized
// dimension/biome documents, version-scoped resource attribute schemas,
// transactional OSS deletion outbox jobs, consumable comment reports, and
// directional mod relationships with editable inverse projections, plus the
// shared reviewed publication model for tutorials, issue reports, news, and
// bounty-backed questions, with
// submitter-attributed history for manual edits and imported resource data,
// plus exclusive pending-review locks and completion subscriptions, and the
// shared localized catalog for plugins, maps, resource packs, shader packs,
// datapacks and add-on resources, including their shared CurseForge/Modrinth
// metadata import task pipeline.
// It also adds owner-scoped editor drafts whose retention is controlled by a
// numeric permission measured in seconds. Submitted snapshots remain grouped
// by project and follow their authoritative review request status.
// Resource attribute fields default to human-editable and may be explicitly
// marked read-only by administrators; import aliases remain independent.
// Searchable records are projected through a durable, coalescing queue so the
// optional Typesense service never participates in content transactions.
// User activity is stored as compact numeric relations without public-ID
// snapshots or JSON metadata; high-frequency editor autosaves are excluded.
// Non-view actions enter a compact durable outbox before fixed-size ingestion;
// views use a bounded best-effort buffer with explicit overload accounting.
// User profile contributions are incrementally persisted as daily aggregates;
// favorite collections have explicit public/private visibility.
// Top-level projects and servers share normalized ratings, dimension scores,
// deduplicated effective views, decaying trend events, Bayesian quality,
// separately scoped project/server promotion items and persisted popularity
// components. Root comments use a trust- and decay-aware heat aggregate.
// They also share reviewed, localized, draft-aware release logs with reusable
// per-project categories and immutable submitter-attributed history.
// All public resources share a persisted metrics projection. View and edit
// bursts are coalesced through a durable numeric-route refresh queue; project
// totals include their bound child resources without synchronous fan-out.
// The administration workbench reads persisted daily site metrics and project
// popularity snapshots instead of aggregating the activity stream on demand.
// User contribution counters are persisted independently from the raw activity
// retention window. It also adds category-aware community catalogs, per-session
// online presence with private-by-default disclosure, configurable public user
// cards, and audited action-specific activity cleanup.
// It also adds independent anti-abuse events, content fingerprints, one-time
// form/challenge tokens, user risk state, temporary restrictions, bot access
// rules, and bounded daily security aggregates.
// Generation 77 adds directional user blacklists used by comments, following,
// and direct messaging, and removes a parameter/column ambiguity from comment
// popularity route resolution. Generation 78 adds normalized recipe-version
// bindings, immutable per-target comment floors, and sanitized log shares.
// Generation 79 adds complete leading indexes for the generation 78 foreign
// keys after the database query-plan audit rejected partial indexes as FK
// maintenance coverage.
// Generation 81 introduces the seeded autobot service identity for early-data
// ingestion and project automation, records that identity on worker runs, and
// persists reversible inactivity-based project maintenance status decisions.
// Generation 82 adds localized notification snapshots, deterministic
// collection-to-mrpack export jobs, MODID import confirmations, the sticker
// catalog and unified project update subscriptions.
// Generation 83 separates authentication revocation from authorization
// changes and records catalog and Minecraft server submitters without granting
// project access.
// Generation 84 removes project-developer and team-claim shortcuts. Project
// access is now derived exclusively from approved editor assignments or an
// approved personal-author claim connected through verified author/team
// relations, without materializing one permanent role per project.
// Generation 85 adds quota-backed comment attachments shared by top-level
// comments and replies. Ordinary files are downloadable only after a clean
// scan; recognized log archives are routed exclusively through redaction.
// Generation 86 establishes checked economy domains and database-level
// conservation constraints for balances, purchases, and question bounties.
// Generation 87 separates manual, account-status, governance, progression,
// preference, and seed authorization sources so independent grants cannot
// overwrite or revoke one another.
// Generation 88 makes executable shop item kinds a closed domain and assigns
// heat-promotion decay sequences through an atomic per-target counter.
// Generation 89 limits favorite records to the three product-supported target
// families; application reads and writes independently revalidate visibility.
// Generation 90 gives each sticker image one exclusive owner and projects all
// current and immutable historical Markdown sticker tokens into an indexed,
// transactionally synchronized reference relation.
// Generation 91 gives user-owned drafts an application and database payload
// budget; per-user count and byte quotas are reserved under a transaction lock.
// Generation 92 requires exactly one server-owned review authority for each
// completed draft and adds separate active/completed keyset page indexes.
// Generation 93 makes catalog-import package source files immutable and
// records server-side content-hash verification without global SHA ownership.
// Generation 94 adds bounded AI task recovery and aggregate Outbox lookup
// indexes for orphaned queued attempts and stale worker executions.
// Generation 95 gives blueprint conversion jobs bounded owner leases,
// heartbeat recovery, and one active job per operation/format.
// Generation 96 registers blueprint-derived OSS objects before upload and
// atomically activates or durably compensates them with their owning attempt.
// Generation 97 gives OSS deletion jobs bounded owner leases, classified dead
// state, audited replay, and safe new deletion cycles for reused object keys.
// Generation 98 replaces the process-local gallery rehome queue with durable,
// generation-aware jobs, bounded leases, dead state, and audited replay.
// Generation 99 persists multipart upload ownership and expiration so
// abandoned provider uploads are actively aborted by a bounded lease worker.
// Generation 100 gives public catalog caches an O(1), transactionally bumped
// dataset version and makes canonical recipe definitions the public authority.
// Generation 101 gives imported mod-content detail projections explicit source
// ownership so re-imports can remove stale importer-owned facts without
// overwriting manual edits. Generation 102 adds the stable status/time/ID index
// required to traverse every project-authorship review state with a keyset
// cursor instead of a fixed queue window. Generation 103 adds the equivalent
// status/time/ID index for the Minecraft server review queue. Generation 104
// gives Markdown playground drafts a monotonic revision and per-editor save
// sequence so request arrival order cannot replace newer content. Generation
// 105 gives follower and following feeds stable time/ID keyset indexes.
// Generation 106 separates display-only contributor/leader roles from the
// explicit owner/developer/maintainer project-access whitelist. Generation
// 107 preserves action-granular statistics baselines before raw activity
// deletion so reconciliation can replace drift exactly instead of only
// increasing counters.
// Generation 108 makes the four-level mod-content category boundary an
// authoritative subtree invariant for inserts and moves.
// Generation 109 gives versioned mod-content placements a transactionally
// maintained GIN full-text search projection for bounded public section queries.
// Generation 110 gives batched mod-content reference resolution case-folded
// resource canonical, alias, and tag lookup indexes.
// Generation 111 coalesces project ACL invalidation once per transaction and
// creator-binding search projection once per affected project statement.
// Generation 112 makes the server search projection authoritative for public
// catalog filters and stable sort keys, including popularity refreshes.
// Generation 113 adds an event-first notification index for bounded project
// update reconciliation. Generation 114 gives seed-crawler run and candidate
// administration stable created/download keyset indexes, including a
// status-leading candidate variant.
// Generation 115 adds stable notification recipient/broadcast page indexes
// and a per-user read watermark so read-all is constant-write. Generation 116
// adds an exact singleton broadcast count and ID-leading partial index so
// unread calibration never rescans shared broadcasts once per active user.
// Generation 117 gives direct conversations and messages bounded member/ID
// keyset pages, an exact per-conversation unread projection, and a persisted
// last-message pointer so list reads perform no per-row history aggregation.
// Generation 118 gives administrative log sources stable time/ID keyset pages,
// write-maintained full-text projections, and ID-stable bounded retention scans.
// Generation 119 replaces per-project popularity lifetime rescans with
// write-maintained counters, bounded trend inputs, and indexed decay scheduling.
// Generation 120 makes per-type global rating totals write-maintained and
// removes their full scan from the per-project popularity worker.
// Generation 121 gives the administrative project workbench a write-maintained
// search/sort projection and stable heat/time/ID keyset pages.
// Generation 122 gives external search rebuilds stable ID batches, bounded
// import memory, and durable per-collection progress.
// Generation 124 gives favorite modpack export history stable owner/status
// keyset pages. Generation 123 added bounded dependency export graph reads and
// cached loader metadata resolution.
// Generation 125 gives blueprint job admission a bounded creator-active
// lookup while blueprint processing enforces shared memory and CPU budgets.
// Generation 132 gives shared content histories stable time/identity keyset
// indexes for manual revisions and imported resource snapshots.
// Generation 133 versions level configuration and moves full-user level/role
// recalculation into durable, leased, primary-key cursor batches.
// Generation 134 gives public and administrative site changelogs one stable
// date/ID keyset contract and indexes the complete administrative history.
// Generation 135 projects both unresolved-reference stores into one bounded,
// trigger-maintained administrative keyset and prefix-search catalog.
// Generation 136 gives reporter histories, moderation queues and ban histories
// strict time/ID keyset pages, adding the missing reporter-leading index.
// Generation 149 replaces the lossy single-owner popularity projection with
// complete effective-developer membership for incremental facts, trend events,
// offline calibration, global rating totals, and the public developer list.
// Existing development popularity facts must be rebuilt by the generation
// reset because their excluded actor cannot be recovered from aggregate rows.
// Generation 150 persists short-lived, digest-bound favorite MRPack preflight
// snapshots so task creation consumes the exact loader and file selection the
// user confirmed instead of rebuilding against a drifting catalog.
// Generation 151 detaches retained favorite MRPack tasks and reports from a
// deleted source collection while cancelling active work transactionally.
// Generation 148 gives seed-crawler auto-submission an explicit recoverable
// submitting state while project creation, source binding, draft completion,
// and candidate completion commit in one transaction. Generation 147 gives
// each seed-crawler candidate explicit immutable first-
// seen and payload-authoritative last-seen run provenance. Generation 146
// binds an external-release manual override to the approved
// user revision that first transferred its changelog away from source control.
// Generation 145 makes project-download publication a scan-gated state
// transition and emits public update events only for safe active files.
// Generation 144 gives each Minecraft server/mod identity independent manual
// and machine evidence so complete probe snapshots can replace machine facts
// without removing declarations or retaining stale machine-only identities.
// Generation 143 makes the product modpack category registry authoritative in
// both request validation and a named database CHECK constraint.
// Generation 142 gives Minecraft loader artifacts a durable, source-attributed
// snapshot keyed by the exact synchronized compatibility catalog.
// Generation 141 removes the redundant single-column log-share owner index;
// its nullable foreign-key probes are covered by the owner-leading partial
// history index whose predicate is exactly owner_user_id IS NOT NULL.
// Generation 140 removes the unused PostgreSQL chat-presence table; Redis with
// a bounded in-process fallback remains the sole runtime presence authority.
//
// Generation 139 removes unreachable generic public routes for project files;
// file downloads remain scoped to their owning project's active-file endpoint.
//
// Generation 138 scopes automated project mirror identity to the provider file
// instead of rejecting distinct files that happen to have identical bytes.
//
// Generation 137 removes four non-unique catalog and mod-content indexes whose
// ordered columns exactly duplicate indexes owned by unique constraints.
// Generation 155 makes sanitized Minecraft texture blobs system-owned shared
// storage facts, maintains a calibratable active-reference projection, and
// indexes bounded garbage-collection candidates.
// Generation 74 repaired the popularity refresh function
// without replacing its persisted facts or queue. Earlier development data is
// intentionally not migrated and must be reset before installation.
func Migrate(ctx context.Context, db *pgxpool.Pool) error {
	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err = conn.Exec(ctx, `select pg_advisory_lock(hashtext('mcmods-cn-schema-migrations'))`); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer conn.Exec(context.Background(), `select pg_advisory_unlock(hashtext('mcmods-cn-schema-migrations'))`)

	var hasSchemaMetadata bool
	if err = conn.QueryRow(ctx, `select to_regclass('public.schema_metadata') is not null`).Scan(&hasSchemaMetadata); err != nil {
		return fmt.Errorf("inspect schema generation: %w", err)
	}
	currentGeneration := 0
	if hasSchemaMetadata {
		err = conn.QueryRow(ctx, `select coalesce((select generation from schema_metadata where singleton),0)`).Scan(&currentGeneration)
	}
	if err != nil {
		return fmt.Errorf("read schema generation: %w", err)
	}
	if currentGeneration != 0 && currentGeneration != schemaGeneration {
		return fmt.Errorf("database schema generation %d is incompatible with generation %d; reset the development database", currentGeneration, schemaGeneration)
	}
	if currentGeneration == schemaGeneration {
		return nil
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin schema installation: %w", err)
	}
	defer tx.Rollback(ctx)
	statements := schemaInstallationStatements()
	for _, statement := range statements {
		if _, err = tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("install schema generation %d: %w", schemaGeneration, err)
		}
	}
	if _, err = tx.Exec(ctx, `create table schema_metadata (
		singleton boolean primary key default true check(singleton),
		generation integer not null,
		installed_at timestamptz not null default now()
	)`); err != nil {
		return fmt.Errorf("create schema metadata: %w", err)
	}
	if _, err = tx.Exec(ctx, `insert into schema_metadata(singleton,generation) values(true,$1)`, schemaGeneration); err != nil {
		return fmt.Errorf("record schema generation: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit schema generation: %w", err)
	}
	return nil
}

func schemaInstallationStatements() []string {
	statements := make([]string, 0, 256)
	statements = append(statements, baselineSchemaStatements()...)
	statements = append(statements, catalogImportArtifactSchemaStatements()...)
	statements = append(statements, catalogSchemaStatements()...)
	statements = append(statements, catalogEditorSchemaStatements()...)
	statements = append(statements, skinSchemaStatements()...)
	statements = append(statements, reviewSchemaStatements()...)
	statements = append(statements, immutableHistoryGuardStatements()...)
	statements = append(statements, blueprintRelationSchemaStatements()...)
	statements = append(statements, communitySchemaStatements()...)
	statements = append(statements, projectFileSchemaStatements()...)
	statements = append(statements, modContentSchemaStatements()...)
	statements = append(statements, commentSchemaStatements()...)
	statements = append(statements, communityPostSchemaStatements()...)
	statements = append(statements, modpackSchemaStatements()...)
	statements = append(statements, simpleProjectSchemaStatements()...)
	statements = append(statements, serverSchemaStatements()...)
	statements = append(statements, ratingSchemaStatements()...)
	statements = append(statements, skinCatalogSchemaStatements()...)
	statements = append(statements, favoritePaginationSchemaStatements()...)
	statements = append(statements, communityPostCatalogSchemaStatements()...)
	statements = append(statements, changelogSchemaStatements()...)
	statements = append(statements, contentMetricsSchemaStatements()...)
	statements = append(statements, adminDashboardSchemaStatements()...)
	statements = append(statements, draftSchemaStatements()...)
	statements = append(statements, searchSchemaStatements()...)
	statements = append(statements, userFeatureSchemaStatements()...)
	statements = append(statements, userBlockSchemaStatements()...)
	statements = append(statements, antiAbuseSchemaStatements()...)
	statements = append(statements, infrastructureSchemaStatements()...)
	statements = append(statements, projectAccessSchemaStatements()...)
	statements = append(statements, featureUpdateSchemaStatements()...)
	statements = append(statements, governanceAutomationSchemaStatements()...)
	statements = append(statements, unresolvedReferenceCatalogSchemaStatements()...)
	statements = append(statements, engagementExportSchemaStatements()...)
	statements = append(statements, stickerReferenceSchemaStatements()...)
	return append(statements, foreignKeyIndexStatement())
}

func foreignKeyIndexStatement() string {
	return `do $$
	declare
		foreign_key record;
		index_name text;
	begin
		for foreign_key in
			select
				constraint_row.conname,
				source.relname table_name,
				constraint_row.conrelid table_oid,
				string_agg(format('%I',attribute_row.attname),',' order by key_row.position) columns_sql
			from pg_constraint constraint_row
			join pg_class source on source.oid=constraint_row.conrelid
			join pg_namespace namespace_row on namespace_row.oid=source.relnamespace
			cross join lateral unnest(constraint_row.conkey) with ordinality key_row(attnum,position)
			join pg_attribute attribute_row
			  on attribute_row.attrelid=constraint_row.conrelid
			 and attribute_row.attnum=key_row.attnum
			where constraint_row.contype='f'
			  and namespace_row.nspname='public'
			  and not exists (
				select 1
				from pg_index index_row
				where index_row.indrelid=constraint_row.conrelid
				  and index_row.indisvalid
				  and index_row.indisready
				  and (
					index_row.indpred is null
					or (
					  cardinality(constraint_row.conkey)=1
					  and pg_get_expr(index_row.indpred,index_row.indrelid)=format(
						'(%I IS NOT NULL)',
						(select predicate_attribute.attname
						 from pg_attribute predicate_attribute
						 where predicate_attribute.attrelid=constraint_row.conrelid
						   and predicate_attribute.attnum=constraint_row.conkey[1])
					  )
					)
				  )
				  and index_row.indexprs is null
				  and index_row.indnkeyatts>=cardinality(constraint_row.conkey)
				  and not exists (
					select 1
					from generate_subscripts(constraint_row.conkey,1) position
					where (index_row.indkey::smallint[])[position-1]<>constraint_row.conkey[position]
				  )
			  )
			group by constraint_row.conname,source.relname,constraint_row.conrelid
			order by source.relname,constraint_row.conname
		loop
			index_name := left('idx_fk_'||foreign_key.table_name||'_'||foreign_key.conname,54)
				||'_'||substr(md5(foreign_key.conname),1,8);
			execute format(
				'create index %I on %s (%s)',
				index_name,
				foreign_key.table_oid::regclass,
				foreign_key.columns_sql
			);
		end loop;
	end $$`
}

func blueprintRelationSchemaStatements() []string {
	return []string{
		`create table if not exists blueprint_mods (
			blueprint_id bigint not null references blueprints(id) on delete cascade,
			source_namespace text not null,
			mod_id bigint not null references mods(id) on delete cascade,
			created_at timestamptz not null default now(),
			primary key (blueprint_id, source_namespace)
		)`,
		`create index if not exists idx_blueprint_mods_mod_blueprint on blueprint_mods(mod_id, blueprint_id)`,
	}
}

func ResetDevelopmentSchema(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, resetDevelopmentSchemaStatement()); err != nil {
		return fmt.Errorf("drop development database objects: %w", err)
	}
	return tx.Commit(ctx)
}

func resetDevelopmentSchemaStatement() string {
	return `do $$
	declare object record;
	begin
		for object in
			select table_name name,'table' kind from information_schema.tables
			where table_schema='public' and table_type='BASE TABLE'
			union all
			select table_name name,'view' kind from information_schema.views where table_schema='public'
		loop
			execute format('drop %s if exists public.%I cascade',object.kind,object.name);
		end loop;
		for object in select sequence_name name from information_schema.sequences where sequence_schema='public'
		loop
			execute format('drop sequence if exists public.%I cascade',object.name);
		end loop;
		for object in
			select
				procedure_row.proname name,
				case procedure_row.prokind when 'p' then 'procedure' else 'function' end kind,
				pg_get_function_identity_arguments(procedure_row.oid) arguments
			from pg_proc procedure_row
			join pg_namespace namespace_row on namespace_row.oid=procedure_row.pronamespace
			where namespace_row.nspname='public'
			  and procedure_row.prokind in ('f','p')
			  and not exists (
				select 1
				from pg_depend dependency_row
				where dependency_row.classid='pg_proc'::regclass
				  and dependency_row.objid=procedure_row.oid
				  and dependency_row.deptype='e'
			  )
		loop
			execute format(
				'drop %s if exists public.%I(%s) cascade',
				object.kind,
				object.name,
				object.arguments
			);
		end loop;
	end $$`
}

func reviewSchemaStatements() []string {
	return []string{
		`create table content_revisions (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			entity_type text,
			entity_id bigint,
			aggregate_type text not null,
			aggregate_key text not null,
			revision_no bigint not null,
			base_revision_id bigint references content_revisions(id) on delete restrict,
			schema_version integer not null default 1,
			snapshot jsonb not null,
			snapshot_hash text not null,
			created_by bigint references users(id) on delete set null,
			created_by_snapshot text not null default '',
			source text not null default 'user',
			created_at timestamptz not null default now(),
			unique(aggregate_type,aggregate_key,revision_no),
			foreign key(entity_type,entity_id) references public_routes(entity_type,internal_id) on delete restrict,
			check((entity_type is null)=(entity_id is null))
		)`,
		`create index idx_content_revisions_entity on content_revisions(entity_type,entity_id,revision_no desc) where entity_id is not null`,
		`create index idx_content_revisions_aggregate on content_revisions(aggregate_type,aggregate_key,revision_no desc)`,
		`create index idx_content_revisions_history on content_revisions(aggregate_type,aggregate_key,created_at desc,public_id desc)`,
		`create table change_requests (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			entity_type text,
			entity_id bigint,
			aggregate_type text not null,
			aggregate_key text not null,
			base_revision_id bigint references content_revisions(id) on delete restrict,
			proposed_revision_id bigint not null unique references content_revisions(id) on delete restrict,
			status text not null default 'pending',
			reason text not null default '',
			submitted_by bigint references users(id) on delete set null,
			submitted_by_snapshot text not null default '',
			metadata jsonb not null default '{}'::jsonb,
			submitted_at timestamptz not null default now(),
			resolved_at timestamptz,
			check(status in ('pending','approved','rejected','conflicted','withdrawn')),
			foreign key(entity_type,entity_id) references public_routes(entity_type,internal_id) on delete restrict,
			check((entity_type is null)=(entity_id is null))
		)`,
		`create index idx_change_requests_queue on change_requests(status,submitted_at,id)`,
		`create unique index idx_change_requests_pending_aggregate
			on change_requests(aggregate_type,aggregate_key) where status='pending'`,
		`create table review_completion_subscriptions (
			change_request_id bigint not null references change_requests(id) on delete cascade,
			user_id bigint not null references users(id) on delete cascade,
			target_label text not null default '',
			target_url text not null default '',
			created_at timestamptz not null default now(),
			notified_at timestamptz,
			primary key(change_request_id,user_id)
		)`,
		`create index idx_review_completion_subscriptions_user
			on review_completion_subscriptions(user_id,created_at desc)`,
		`create table review_events (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			change_request_id bigint not null references change_requests(id) on delete restrict,
			event_type text not null,
			actor_id bigint references users(id) on delete set null,
			actor_snapshot text not null default '',
			note text not null default '',
			ip text not null default '',
			user_agent text not null default '',
			metadata jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now(),
			check(event_type in ('submitted','approved','rejected','conflicted','withdrawn','published'))
		)`,
		`create index idx_review_events_request_created on review_events(change_request_id,created_at,id)`,
		`create table content_change_items (
			id bigserial primary key,
			revision_id bigint not null references content_revisions(id) on delete restrict,
			path text not null,
			operation text not null,
			before_value jsonb,
			after_value jsonb,
			check(operation in ('add','remove','replace'))
		)`,
		`create index idx_content_change_items_revision on content_change_items(revision_id,id)`,
		`create table audit_events (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			entity_type text,
			entity_id bigint,
			aggregate_type text not null,
			aggregate_key text not null,
			actor_id bigint references users(id) on delete set null,
			actor_snapshot text not null default '',
			action text not null,
			before_hash text not null default '',
			after_hash text not null default '',
			trace_id text not null default '',
			ip text not null default '',
			user_agent text not null default '',
			metadata jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now(),
			foreign key(entity_type,entity_id) references public_routes(entity_type,internal_id) on delete restrict,
			check((entity_type is null)=(entity_id is null))
		)`,
		`create index idx_audit_events_entity on audit_events(entity_type,entity_id,created_at desc,id desc) where entity_id is not null`,
		`create index idx_audit_events_aggregate on audit_events(aggregate_type,aggregate_key,created_at desc,id desc)`,
		`alter table catalog_entities add constraint fk_catalog_entities_published_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table content_localizations add constraint fk_content_localizations_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table catalog_resource_definitions add constraint fk_catalog_resource_definitions_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table catalog_tag_members add constraint fk_catalog_tag_members_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table recipe_type_definitions add constraint fk_recipe_type_definitions_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table recipe_type_catalysts add constraint fk_recipe_type_catalysts_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table recipe_layout_templates add constraint fk_recipe_layout_templates_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table recipe_definitions add constraint fk_recipe_definitions_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table knowledge_pages add constraint fk_knowledge_pages_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table recipe_content_overrides add constraint fk_recipe_content_overrides_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table users add constraint fk_users_avatar_file foreign key(avatar_file_id) references oss_files(id) on delete set null`,
		`alter table mod_relationships add constraint fk_mod_relationships_group foreign key(group_id) references mod_relationship_groups(id) on delete cascade`,
		`alter table mods add constraint fk_mods_published_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table mod_gallery_images add constraint fk_mod_gallery_images_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table skin_assets add constraint fk_skin_assets_published_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
		`alter table users add constraint fk_users_profile_revision foreign key(profile_revision_id) references content_revisions(id) on delete restrict`,
		`alter table blueprints add constraint fk_blueprints_published_revision foreign key(published_revision_id) references content_revisions(id) on delete restrict`,
	}
}

func immutableHistoryGuardStatements() []string {
	return []string{
		`create or replace function prevent_immutable_history_mutation() returns trigger as $$
		begin raise exception '% is append-only',tg_table_name; end;
		$$ language plpgsql`,
		`create trigger trg_content_revisions_immutable before update or delete on content_revisions for each row execute function prevent_immutable_history_mutation()`,
		`create trigger trg_review_events_immutable before update or delete on review_events for each row execute function prevent_immutable_history_mutation()`,
		`create trigger trg_content_change_items_immutable before update or delete on content_change_items for each row execute function prevent_immutable_history_mutation()`,
		`create trigger trg_audit_events_immutable before update or delete on audit_events for each row execute function prevent_immutable_history_mutation()`,
		`create trigger trg_permission_audit_logs_immutable before update or delete on permission_audit_logs for each row execute function prevent_immutable_history_mutation()`,
	}
}
