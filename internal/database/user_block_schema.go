package database

// userBlockSchemaStatements installs the directional user blacklist relation.
// PostgreSQL remains the source of truth because this relation controls
// authorization as well as presentation.
func userBlockSchemaStatements() []string {
	return []string{
		`create table if not exists user_blocks (
			blocker_id bigint not null references users(id) on delete cascade,
			blocked_id bigint not null references users(id) on delete cascade,
			created_at timestamptz not null default now(),
			primary key(blocker_id,blocked_id),
			check(blocker_id<>blocked_id)
		)`,
		`create index if not exists idx_user_blocks_blocked_blocker
			on user_blocks(blocked_id,blocker_id)`,
	}
}
