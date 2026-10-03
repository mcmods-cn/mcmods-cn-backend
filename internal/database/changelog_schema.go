package database

// changelogSchemaStatements installs the shared release-log model used by all
// top-level project families and Minecraft servers. Target and entry identity
// stay numeric internally; public IDs only cross the HTTP boundary.
func changelogSchemaStatements() []string {
	return []string{
		`create table project_changelog_categories (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			object_route_id bigint not null references public_routes(id) on delete cascade,
			default_locale text not null,
			created_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`create index idx_project_changelog_categories_target
			on project_changelog_categories(object_route_id,created_at,id)`,
		`create table project_changelog_category_localizations (
			category_id bigint not null references project_changelog_categories(id) on delete cascade,
			locale text not null,
			name text not null check(char_length(name) between 1 and 80),
			primary key(category_id,locale)
		)`,
		`create or replace function register_project_changelog_category_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id,canonical_path)
			values(new.public_id,'project_changelog_category',new.id,'/changelogs/categories/'||new.public_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_project_changelog_categories_public_route after insert on project_changelog_categories
			for each row execute function register_project_changelog_category_public_route()`,
		`create or replace function remove_project_changelog_category_public_route() returns trigger as $$
		begin
			delete from public_routes where entity_type='project_changelog_category' and internal_id=old.id;
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_project_changelog_categories_remove_public_route after delete on project_changelog_categories
			for each row execute function remove_project_changelog_category_public_route()`,
		`create table project_changelogs (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			object_route_id bigint not null references public_routes(id) on delete cascade,
			category_id bigint references project_changelog_categories(id) on delete set null,
			event_at timestamptz not null,
			minecraft_versions text[] not null default '{}'::text[],
			project_version text not null check(char_length(project_version) between 1 and 120),
			default_locale text not null,
			status text not null default 'active' check(status in ('active','deleted')),
			review_status text not null default 'pending' check(review_status in ('pending','approved','rejected')),
			published_revision_id bigint references content_revisions(id) on delete restrict,
			created_by bigint not null references users(id) on delete restrict,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			published_at timestamptz
		)`,
		`create index idx_project_changelogs_target
			on project_changelogs(object_route_id,review_status,event_at desc,id desc) where status='active'`,
		`create index idx_project_changelogs_category
			on project_changelogs(category_id,event_at desc,id desc) where status='active'`,
		`create index idx_project_changelogs_author
			on project_changelogs(created_by,created_at desc,id desc)`,
		`create table project_changelog_localizations (
			changelog_id bigint not null references project_changelogs(id) on delete cascade,
			locale text not null,
			body_markdown text not null,
			source_revision_id bigint not null references content_revisions(id) on delete restrict,
			updated_by bigint references users(id) on delete set null,
			updated_at timestamptz not null default now(),
			primary key(changelog_id,locale)
		)`,
		`create or replace function register_project_changelog_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id,canonical_path)
			values(new.public_id,'project_changelog',new.id,'/changelogs/'||new.public_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_project_changelogs_public_route after insert on project_changelogs
			for each row execute function register_project_changelog_public_route()`,
		`create or replace function remove_project_changelog_public_route() returns trigger as $$
		begin
			delete from public_routes where entity_type='project_changelog' and internal_id=old.id;
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_project_changelogs_remove_public_route after delete on project_changelogs
			for each row execute function remove_project_changelog_public_route()`,
		changelogPopularityFunctionSQL,
		`create trigger trg_project_changelogs_popularity
			after update of review_status,status on project_changelogs
			for each row execute function record_changelog_popularity_event()`,
	}
}
