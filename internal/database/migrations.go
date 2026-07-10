package database

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Migrate(ctx context.Context, db *pgxpool.Pool) error {
	statements := []string{
		`create table if not exists users (
			id bigserial primary key,
			username text not null unique,
			email text not null unique,
			display_name text not null,
			password_hash text not null,
			email_verified boolean not null default false,
			status text not null default 'active',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			last_login_at timestamptz
		)`,
		`create table if not exists roles (
			id bigserial primary key,
			code text not null unique,
			name text not null,
			description text not null default '',
			weight integer not null default 0,
			parents text[] not null default '{}'::text[],
			created_at timestamptz not null default now()
		)`,
		`alter table roles add column if not exists weight integer not null default 0`,
		`alter table roles add column if not exists parents text[] not null default '{}'::text[]`,
		`alter table roles add column if not exists status text not null default 'active'`,
		`alter table roles add column if not exists updated_at timestamptz not null default now()`,
		`alter table roles add column if not exists translations jsonb not null default '{}'::jsonb`,
		`alter table users add column if not exists country text not null default ''`,
		`alter table users add column if not exists timezone text not null default 'Asia/Shanghai'`,
		`alter table users add column if not exists preferred_content_language text not null default 'zh-CN'`,
		`alter table users add column if not exists preferred_ui_language text not null default 'en'`,
		`alter table users add column if not exists security_score integer not null default 100`,
		`create table if not exists permissions (
			id bigserial primary key,
			code text not null unique,
			module text not null,
			name text not null,
			description text not null default ''
		)`,
		`alter table permissions add column if not exists translations jsonb not null default '{}'::jsonb`,
		`create table if not exists role_permissions (
			role_id bigint not null references roles(id) on delete cascade,
			permission_id bigint not null references permissions(id) on delete cascade,
			primary key (role_id, permission_id)
		)`,
		`alter table role_permissions add column if not exists allow boolean not null default true`,
		`alter table role_permissions add column if not exists expires_at timestamptz`,
		`alter table role_permissions add column if not exists updated_at timestamptz not null default now()`,
		`create table if not exists user_role_bindings (
			user_id bigint not null references users(id) on delete cascade,
			role_id bigint not null references roles(id) on delete cascade,
			created_at timestamptz not null default now(),
			primary key (user_id, role_id)
		)`,
		`create table if not exists user_permissions (
			user_id bigint not null references users(id) on delete cascade,
			permission_id bigint not null references permissions(id) on delete cascade,
			allow boolean not null default true,
			context text not null default '',
			expires_at timestamptz,
			created_at timestamptz not null default now(),
			primary key (user_id, permission_id)
		)`,
		`alter table user_permissions add column if not exists context text not null default ''`,
		`alter table user_permissions add column if not exists updated_at timestamptz not null default now()`,
		`create table if not exists permission_audit_logs (
			id bigserial primary key,
			operator_id bigint references users(id) on delete set null,
			target_user_id bigint references users(id) on delete set null,
			action text not null,
			payload jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now()
		)`,
		`create table if not exists user_login_logs (
			id bigserial primary key,
			user_id bigint references users(id) on delete set null,
			account text not null,
			ip text not null default '',
			user_agent text not null default '',
			success boolean not null,
			reason text not null default '',
			created_at timestamptz not null default now()
		)`,
		`create table if not exists email_verification_codes (
			id bigserial primary key,
			email text not null,
			purpose text not null,
			code_hash text not null,
			expires_at timestamptz not null,
			consumed_at timestamptz,
			created_at timestamptz not null default now()
		)`,
		`create index if not exists idx_email_verification_codes_lookup on email_verification_codes (email, purpose, expires_at desc)`,
		`create table if not exists system_settings (
			key text primary key,
			value jsonb not null,
			updated_by bigint references users(id) on delete set null,
			updated_at timestamptz not null default now()
		)`,
		`create table if not exists oauth_accounts (
			id bigserial primary key,
			user_id bigint not null references users(id) on delete cascade,
			provider text not null,
			provider_user_id text not null,
			username text not null default '',
			email text not null default '',
			avatar_url text not null default '',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			unique (provider, provider_user_id)
		)`,
		`create index if not exists idx_oauth_accounts_user_id on oauth_accounts (user_id)`,
		`create table if not exists oss_files (
			id bigserial primary key,
			bucket text not null default '',
			endpoint text not null default '',
			region text not null default '',
			object_key text not null unique,
			category text not null default '',
			source text not null default '',
			original_name text not null default '',
			content_type text not null default '',
			size_bytes bigint not null default 0,
			sha256 text not null default '',
			uploader_id bigint references users(id) on delete set null,
			status text not null default 'active',
			scan_status text not null default 'pending',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`create index if not exists idx_oss_files_category on oss_files (category, created_at desc)`,
		`create index if not exists idx_oss_files_uploader_created_at on oss_files (uploader_id, created_at desc)`,
		`create index if not exists idx_oss_files_object_key_prefix on oss_files (object_key text_pattern_ops)`,
		`create index if not exists idx_oss_files_sha256_size on oss_files (sha256, size_bytes) where sha256 <> ''`,
		`create table if not exists markdown_playground_drafts (
			user_id bigint primary key references users(id) on delete cascade,
			content text not null default '',
			updated_at timestamptz not null default now()
		)`,
		`create table if not exists oss_upload_logs (
			id bigserial primary key,
			file_id bigint references oss_files(id) on delete set null,
			uploader_id bigint references users(id) on delete set null,
			object_key text not null default '',
			original_name text not null default '',
			size_bytes bigint not null default 0,
			ip text not null default '',
			user_agent text not null default '',
			result text not null default 'success',
			message text not null default '',
			created_at timestamptz not null default now()
		)`,
		`create index if not exists idx_oss_upload_logs_created_at on oss_upload_logs (created_at desc)`,
		`create table if not exists oss_scan_logs (
			id bigserial primary key,
			file_id bigint references oss_files(id) on delete set null,
			object_key text not null default '',
			engine text not null default '',
			result text not null default 'pending',
			message text not null default '',
			payload jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now()
		)`,
		`create index if not exists idx_oss_scan_logs_created_at on oss_scan_logs (created_at desc)`,
		`create table if not exists oss_download_stats (
			object_key text primary key,
			file_id bigint references oss_files(id) on delete set null,
			downloads bigint not null default 0,
			total_bytes bigint not null default 0,
			last_download_at timestamptz
		)`,
		`create table if not exists app_logs (
			id bigserial primary key,
			category text not null,
			level text not null default 'info',
			actor_id bigint references users(id) on delete set null,
			action text not null default '',
			target text not null default '',
			ip text not null default '',
			user_agent text not null default '',
			method text not null default '',
			path text not null default '',
			status integer not null default 0,
			latency_ms bigint not null default 0,
			payload jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now()
		)`,
		`create index if not exists idx_app_logs_category_created_at on app_logs (category, created_at desc)`,
		`create table if not exists ai_tasks (
			id bigserial primary key,
			task_uid text not null unique,
			task_type text not null,
			provider text not null default '',
			model text not null default '',
			status text not null default 'queued',
			priority integer not null default 0,
			concurrency_key text not null default '',
			input_tokens bigint not null default 0,
			output_tokens bigint not null default 0,
			cost_micros bigint not null default 0,
			payload jsonb not null default '{}'::jsonb,
			result jsonb not null default '{}'::jsonb,
			error text not null default '',
			created_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			queued_at timestamptz,
			started_at timestamptz,
			finished_at timestamptz,
			updated_at timestamptz not null default now()
		)`,
		`create index if not exists idx_ai_tasks_status_created_at on ai_tasks (status, created_at desc)`,
		`create index if not exists idx_ai_tasks_type_created_at on ai_tasks (task_type, created_at desc)`,
		`create table if not exists ai_task_logs (
			id bigserial primary key,
			task_id bigint references ai_tasks(id) on delete cascade,
			level text not null default 'info',
			event text not null,
			message text not null default '',
			payload jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now()
		)`,
		`create index if not exists idx_ai_task_logs_task_created_at on ai_task_logs (task_id, created_at desc)`,
	}

	for _, statement := range statements {
		if _, err := db.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
