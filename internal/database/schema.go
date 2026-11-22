package database

func schemaInstallationStatements() []string {
	statements := make([]string, 0, 256)
	statements = append(statements, baselineSchemaStatements()...)
	statements = append(statements, catalogImportArtifactSchemaStatements()...)
	statements = append(statements, catalogSchemaStatements()...)
	statements = append(statements, catalogTagPrefixIndexSQL)
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
	statements = append(statements, NotificationVisibilitySchemaStatements()...)
	statements = append(statements, OSSUploadByteBindingSchemaStatements()...)
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
