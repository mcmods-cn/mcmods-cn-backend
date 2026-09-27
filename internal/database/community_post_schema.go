package database

// communityPostSchemaStatements installs the single publishing model shared by
// tutorials, issue/feature reports, news, and bounty-backed questions.
func communityPostSchemaStatements() []string {
	return []string{
		`create table community_posts (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			kind text not null check(kind in ('tutorial','issue','news','discussion')),
			category text not null,
			author_id bigint not null references users(id) on delete restrict,
			title text not null,
			source_locale text not null,
			body_markdown text not null default '',
			minecraft_versions text[] not null default '{}'::text[],
			mod_version_min text not null default '',
			mod_version_max text not null default '',
			severity text not null default '' check(severity in ('','client','harmless','minor','harmful','severe','fatal')),
			has_fix boolean not null default false,
			issue_url text not null default '',
			cover_file_id bigint references oss_files(id) on delete set null,
			resolution_status text not null default 'open' check(resolution_status in ('open','answered','self_solved')),
			accepted_comment_id bigint references comments(id) on delete restrict,
			resolved_at timestamptz,
			status text not null default 'active' check(status in ('active','deleted')),
			review_status text not null default 'pending' check(review_status in ('pending','approved','rejected')),
			published_revision_id bigint references content_revisions(id) on delete restrict,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			published_at timestamptz,
			check(kind='issue' or (severity='' and mod_version_min='' and mod_version_max='' and not has_fix and issue_url='')),
			check(kind<>'issue' or severity<>''),
			check(kind='discussion' or (resolution_status='open' and accepted_comment_id is null and resolved_at is null)),
			check(resolution_status='answered' or accepted_comment_id is null),
			check((resolution_status='open')=(resolved_at is null))
		)`,
		`create index idx_community_posts_catalog
			on community_posts(kind,review_status,published_at desc,id desc) where status='active'`,
		`create index idx_community_posts_category_catalog
			on community_posts(kind,category,review_status,published_at desc,id desc) where status='active'`,
		`create index idx_community_posts_author
			on community_posts(author_id,created_at desc,id desc)`,
		`create table community_post_bounties (
			post_id bigint primary key references community_posts(id) on delete cascade,
			currency_id bigint not null references currencies(id) on delete restrict,
			amount bigint not null check(amount between 1 and 1000000000000),
			status text not null default 'held' check(status in ('held','awarded','refunded')),
			recipient_id bigint references users(id) on delete restrict,
			tax_amount bigint not null default 0 check(tax_amount between 0 and amount),
			net_amount bigint not null default 0 check(net_amount between 0 and amount),
			created_at timestamptz not null default now(),
			settled_at timestamptz,
			check(status='held' or settled_at is not null),
			check(status<>'awarded' or recipient_id is not null),
			check(status<>'awarded' or tax_amount+net_amount=amount),
			check(status='awarded' or (tax_amount=0 and net_amount=0))
		)`,
		`create table community_post_project_refs (
			id bigserial primary key,
			post_id bigint not null references community_posts(id) on delete cascade,
			target_type text not null,
			target_id bigint,
			raw_identifier text not null default '',
			display_order integer not null default 0,
			foreign key(target_type,target_id) references public_routes(entity_type,internal_id) on delete restrict,
			check((target_id is null)<>(raw_identifier=''))
		)`,
		`create unique index idx_community_post_project_identity
			on community_post_project_refs(post_id,target_type,coalesce(target_id,0),raw_identifier)`,
		`create index idx_community_post_project_target
			on community_post_project_refs(target_type,target_id,post_id) where target_id is not null`,
		`create table community_post_resource_refs (
			id bigserial primary key,
			post_id bigint not null references community_posts(id) on delete cascade,
			resource_id bigint references catalog_entities(id) on delete restrict,
			kind_code text not null,
			raw_resource_id text not null default '',
			display_order integer not null default 0,
			check((resource_id is null)<>(raw_resource_id=''))
		)`,
		`create unique index idx_community_post_resource_identity
			on community_post_resource_refs(post_id,kind_code,coalesce(resource_id,0),raw_resource_id)`,
		`create index idx_community_post_resource_target
			on community_post_resource_refs(resource_id,post_id) where resource_id is not null`,
		`create or replace function remove_community_post_reference_unresolved() returns trigger as $$
		begin
			delete from unresolved_references where source_type=TG_ARGV[0] and source_id=old.id;
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_community_post_project_ref_unresolved after delete on community_post_project_refs
			for each row execute function remove_community_post_reference_unresolved('community_post_project')`,
		`create trigger trg_community_post_resource_ref_unresolved after delete on community_post_resource_refs
			for each row execute function remove_community_post_reference_unresolved('community_post_resource')`,
		`create table community_post_translations (
			post_id bigint not null references community_posts(id) on delete cascade,
			locale text not null,
			title text not null,
			body_markdown text not null default '',
			source_revision_id bigint not null references content_revisions(id) on delete cascade,
			ai_task_id bigint references ai_tasks(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			primary key(post_id,locale)
		)`,
		`create or replace function register_community_post_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id,canonical_path)
			values(new.public_id,'community_post',new.id,
				case new.kind when 'tutorial' then '/tutorials/' when 'issue' then '/issues/'
				when 'news' then '/news/' else '/discussions/' end || new.public_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_community_posts_public_route after insert on community_posts
			for each row execute function register_community_post_public_route()`,
		`create or replace function remove_community_post_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type='community_post';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_community_posts_remove_public_route after delete on community_posts
			for each row execute function remove_community_post_public_route()`,
	}
}
