package database

// commentSchemaStatements installs the shared tree-comment model used by every
// public content page. target_key is intentionally polymorphic: most targets
// use a nine-character public ID, while version-scoped mod resources use
// "<resource public ID>~<content version public ID>".
func commentSchemaStatements() []string {
	return []string{
		`create table comments (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			target_type text not null,
			target_key text not null,
			author_id bigint not null references users(id) on delete restrict,
			parent_id bigint references comments(id) on delete restrict,
			root_id bigint references comments(id) on delete restrict,
			depth integer not null default 0 check(depth between 0 and 256),
			body text not null,
			status text not null default 'published',
			child_count integer not null default 0 check(child_count >= 0),
			descendant_count integer not null default 0 check(descendant_count >= 0),
			idempotency_key text not null default '',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			deleted_at timestamptz,
			check(target_type ~ '^[a-z][a-z0-9_]{1,63}$'),
			check(char_length(target_key) between 1 and 255),
			check(status in ('published','pending','hidden','deleted','spam')),
			check((parent_id is null and root_id is null and depth=0) or
			      (parent_id is not null and root_id is not null and depth>0))
		)`,
		`create unique index idx_comments_author_idempotency
			on comments(author_id,idempotency_key) where idempotency_key<>''`,
		`create index idx_comments_target_roots
			on comments(target_type,target_key,created_at desc,id desc) where parent_id is null`,
		`create index idx_comments_root_created on comments(root_id,created_at,id)`,
		`create index idx_comments_parent_created on comments(parent_id,created_at,id)`,
		`create table comment_closure (
			ancestor_id bigint not null references comments(id) on delete cascade,
			descendant_id bigint not null references comments(id) on delete cascade,
			depth integer not null check(depth between 0 and 256),
			primary key(ancestor_id,descendant_id)
		)`,
		`create index idx_comment_closure_descendant on comment_closure(descendant_id,depth,ancestor_id)`,
		`create table comment_reactions (
			comment_id bigint not null references comments(id) on delete cascade,
			user_id bigint not null references users(id) on delete cascade,
			reaction text not null,
			created_at timestamptz not null default now(),
			primary key(comment_id,user_id,reaction),
			check(reaction in ('thumbs_up','thumbs_down','laugh','hooray','confused','heart','rocket','eyes'))
		)`,
		`create table comment_watches (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			user_id bigint not null references users(id) on delete cascade,
			comment_id bigint not null references comments(id) on delete cascade,
			status text not null default 'active',
			muted_until timestamptz,
			muted_forever boolean not null default false,
			unread_count integer not null default 0 check(unread_count >= 0),
			watched_reply_count integer not null default 0 check(watched_reply_count >= 0),
			last_activity_at timestamptz not null default now(),
			last_read_comment_id bigint references comments(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			cancelled_at timestamptz,
			unique(user_id,comment_id),
			check(status in ('active','cancelled'))
		)`,
		`create index idx_comment_watches_user_activity
			on comment_watches(user_id,status,last_activity_at desc,id desc)`,
		`create index idx_comment_watches_comment_active
			on comment_watches(comment_id,user_id) where status='active'`,
		`create table comment_watch_replies (
			watch_id bigint not null references comment_watches(id) on delete cascade,
			comment_id bigint not null references comments(id) on delete cascade,
			notification_id bigint references notifications(id) on delete set null,
			read_at timestamptz,
			created_at timestamptz not null default now(),
			primary key(watch_id,comment_id)
		)`,
		`create index idx_comment_watch_replies_unread
			on comment_watch_replies(watch_id,created_at,comment_id) where read_at is null`,
		`create table comment_reports (
			id bigserial primary key,
			comment_id bigint not null references comments(id) on delete cascade,
			reporter_id bigint not null references users(id) on delete cascade,
			reason text not null,
			detail text not null default '',
			status text not null default 'pending',
			created_at timestamptz not null default now(),
			resolved_at timestamptz,
			unique(comment_id,reporter_id),
			check(status in ('pending','resolved','dismissed'))
		)`,
		`create index idx_comment_reports_queue on comment_reports(status,created_at,id)`,
		`create or replace function register_comment_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,entity_key,canonical_path)
			values(new.public_id,'comment',new.id::text,'/comments/'||new.public_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_comments_public_route after insert on comments
			for each row execute function register_comment_public_route()`,
	}
}
