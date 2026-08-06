package database

// draftSchemaStatements installs private, user-owned editor snapshots. Drafts
// deliberately do not register public routes: their identifiers are only
// usable through authenticated owner-scoped API endpoints.
func draftSchemaStatements() []string {
	return []string{
		`create table user_drafts (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			user_id bigint not null references users(id) on delete cascade,
			draft_key text not null check(length(draft_key) between 1 and 255),
			project_key text not null check(length(project_key) between 1 and 255),
			project_title text not null default '' check(length(project_title) <= 200),
			kind text not null check(length(kind) between 1 and 64),
			title text not null default '' check(length(title) <= 200),
			edit_url text not null check(length(edit_url) between 1 and 1000),
			target_url text not null default '' check(length(target_url) <= 1000),
			payload jsonb not null check(jsonb_typeof(payload) = 'object'),
			change_request_id bigint references change_requests(id) on delete set null,
			review_target_type text not null default '' check(review_target_type in ('','server')),
			review_target_id bigint references minecraft_servers(id) on delete set null,
			submitted_status text not null default '' check(submitted_status in ('','pending','approved')),
			submitted_at timestamptz,
			expires_at timestamptz not null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check((submitted_at is null and submitted_status='') or (submitted_at is not null and submitted_status<>''))
		)`,
		`create unique index idx_user_drafts_active_key on user_drafts(user_id,draft_key) where submitted_at is null`,
		`create index idx_user_drafts_owner_updated on user_drafts(user_id,updated_at desc,id desc)`,
		`create index idx_user_drafts_owner_project on user_drafts(user_id,project_key,coalesce(submitted_at,updated_at) desc,id desc)`,
		`create index idx_user_drafts_change_request on user_drafts(change_request_id) where change_request_id is not null`,
		`create index idx_user_drafts_review_target on user_drafts(review_target_id) where review_target_id is not null`,
		`create index idx_user_drafts_expiration on user_drafts(expires_at)`,
	}
}
