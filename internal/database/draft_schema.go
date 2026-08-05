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
			kind text not null check(length(kind) between 1 and 64),
			title text not null default '' check(length(title) <= 200),
			edit_url text not null check(length(edit_url) between 1 and 1000),
			payload jsonb not null check(jsonb_typeof(payload) = 'object'),
			expires_at timestamptz not null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			unique(user_id,draft_key)
		)`,
		`create index idx_user_drafts_owner_updated on user_drafts(user_id,updated_at desc,id desc)`,
		`create index idx_user_drafts_expiration on user_drafts(expires_at)`,
	}
}
