package database

// featureUpdateSchemaStatements is part of the authoritative development
// schema. Development databases are reset between incompatible generations,
// so this list contains no historical backfill or dual-read compatibility.
func featureUpdateSchemaStatements() []string {
	return []string{
		`create table if not exists recipe_version_bindings (
			recipe_id bigint not null references recipes(entity_id) on delete cascade,
			version_code text not null,
			created_by bigint references users(id) on delete set null,
			source text not null default 'import',
			created_at timestamptz not null default now(),
			primary key(recipe_id,version_code),
			check(version_code=btrim(version_code) and version_code<>''),
			check(source in ('import','editor'))
		)`,
		`create index if not exists idx_recipe_version_bindings_version
			on recipe_version_bindings(version_code,recipe_id)`,
		`create index if not exists idx_recipe_version_bindings_created_by
			on recipe_version_bindings(created_by)`,
		`alter table comments add column if not exists floor_number bigint`,
		`create table if not exists comment_floor_counters (
			target_type text not null,
			target_id bigint not null,
			target_version_key bigint not null default 0,
			last_floor bigint not null default 0 check(last_floor>=0),
			updated_at timestamptz not null default now(),
			primary key(target_type,target_id,target_version_key)
		)`,
		`create unique index if not exists idx_comments_target_floor
		 on comments(target_type,target_id,coalesce(target_version_id,0),floor_number)
		 where parent_id is null and floor_number is not null`,
		`create index if not exists idx_comments_floor_lookup
		 on comments(target_type,target_id,coalesce(target_version_id,0),floor_number,status)
		 where parent_id is null`,
		`create table if not exists log_shares (
			id bigserial primary key,
			public_code text not null unique check(public_code ~ '^[A-Za-z0-9_-]{12,32}$'),
			owner_user_id bigint references users(id) on delete cascade,
			source_type text not null,
			source_file_id bigint references oss_files(id) on delete set null,
			title text not null default '',
			original_name text not null default '',
			status text not null default 'processing',
			redaction_version integer not null default 1,
			redaction_counts jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now(),
			expires_at timestamptz not null,
			deleted_at timestamptz,
			check(source_type in ('file','paste')),
			check(status in ('processing','ready','failed','expired','deleted','source_deleted')),
			check((source_type='file' and owner_user_id is not null) or
			      (source_type='paste' and source_file_id is null))
		)`,
		`create index if not exists idx_log_shares_owner_created
			on log_shares(owner_user_id,created_at desc,id desc) where owner_user_id is not null`,
		`create index if not exists idx_log_shares_source_file_fk
			on log_shares(source_file_id)`,
		`create index if not exists idx_log_shares_expiry
		 on log_shares(expires_at,id) where status in ('processing','ready')`,
		`create unique index if not exists idx_log_shares_active_source_file
		 on log_shares(source_file_id,redaction_version)
		 where source_file_id is not null and status in ('processing','ready') and deleted_at is null`,
		`create table if not exists log_share_entries (
			id bigserial primary key,
			log_share_id bigint not null references log_shares(id) on delete cascade,
			entry_index integer not null check(entry_index>=0),
			original_name text not null default '',
			safe_display_name text not null default '',
			content_type text not null default 'text/plain',
			sanitized_text text,
			sanitized_object_key text not null default '',
			byte_size bigint not null default 0 check(byte_size>=0),
			line_count bigint not null default 0 check(line_count>=0),
			checksum text not null default '',
			status text not null default 'ready',
			unique(log_share_id,entry_index),
			check(status in ('processing','ready','failed','removed')),
			check((sanitized_text is not null) <> (sanitized_object_key<>''))
		)`,
		`create table if not exists comment_attachments (
			comment_id bigint not null references comments(id) on delete cascade,
			attachment_file_id bigint not null references oss_files(id) on delete cascade,
			kind text not null default 'file' check(kind in ('file','log')),
			processing_status text not null default 'processing' check(processing_status in ('ready','processing','failed')),
			created_at timestamptz not null default now(),
			primary key(comment_id,attachment_file_id)
		)`,
		`create index if not exists idx_comment_attachments_file
			on comment_attachments(attachment_file_id,comment_id)`,
		`create table if not exists comment_log_bindings (
			comment_id bigint not null,
			attachment_file_id bigint not null,
			log_share_id bigint not null references log_shares(id) on delete cascade,
			created_at timestamptz not null default now(),
			primary key(comment_id,attachment_file_id),
			unique(comment_id,log_share_id),
			foreign key(comment_id,attachment_file_id)
				references comment_attachments(comment_id,attachment_file_id) on delete cascade
		)`,
		`create index if not exists idx_comment_log_bindings_share
			on comment_log_bindings(log_share_id,comment_id)`,
		`create index if not exists idx_comment_log_bindings_attachment
			on comment_log_bindings(attachment_file_id,comment_id)`,
		`create table if not exists comment_log_attachment_jobs (
			id bigserial primary key,
			comment_id bigint not null,
			attachment_file_id bigint not null,
			requested_by bigint not null references users(id) on delete cascade,
			status text not null default 'queued',
			attempts integer not null default 0,
			max_attempts integer not null default 5,
			next_attempt_at timestamptz not null default now(),
			lease_expires_at timestamptz,
			locked_by text not null default '',
			last_error text not null default '',
			created_at timestamptz not null default now(),
			started_at timestamptz,
			finished_at timestamptz,
			updated_at timestamptz not null default now(),
			unique(comment_id,attachment_file_id),
			foreign key(comment_id,attachment_file_id)
				references comment_attachments(comment_id,attachment_file_id) on delete cascade,
			check(status in ('queued','processing','completed','failed')),
			check(attempts>=0 and max_attempts>0 and attempts<=max_attempts)
		)`,
		`create index if not exists idx_comment_log_attachment_jobs_pending
			on comment_log_attachment_jobs(next_attempt_at,id) where status='queued'`,
		`create index if not exists idx_comment_log_attachment_jobs_processing
			on comment_log_attachment_jobs(lease_expires_at,id) where status='processing'`,
		`create index if not exists idx_comment_log_attachment_jobs_requester
			on comment_log_attachment_jobs(requested_by,id)`,
	}
}
