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
		`alter table users add column if not exists registration_ip text not null default ''`,
		`alter table users add column if not exists registration_country_code text not null default ''`,
		`alter table users add column if not exists registration_city text not null default ''`,
		`alter table users add column if not exists signature text not null default ''`,
		`alter table users add column if not exists avatar_url text not null default ''`,
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
		`alter table user_login_logs add column if not exists country_code text not null default ''`,
		`alter table user_login_logs add column if not exists city text not null default ''`,
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
		`alter table oss_files add column if not exists source_original_name text not null default ''`,
		`alter table oss_files add column if not exists source_size_bytes bigint not null default 0`,
		`alter table users add column if not exists avatar_file_id bigint references oss_files(id) on delete set null`,
		`create index if not exists idx_oss_files_category on oss_files (category, created_at desc)`,
		`create index if not exists idx_oss_files_uploader_created_at on oss_files (uploader_id, created_at desc)`,
		`create index if not exists idx_oss_files_object_key_prefix on oss_files (object_key text_pattern_ops)`,
		`create index if not exists idx_oss_files_sha256_size on oss_files (sha256, size_bytes) where sha256 <> ''`,
		`create index if not exists idx_oss_files_sha256_source_size on oss_files (sha256, source_size_bytes) where sha256 <> ''`,
		`create table if not exists permission_role_tracks (
			code text primary key,
			name text not null,
			description text not null default '',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`create table if not exists permission_role_track_roles (
			track_code text not null references permission_role_tracks(code) on delete cascade,
			role_id bigint not null references roles(id) on delete cascade,
			position integer not null,
			primary key (track_code, position),
			unique (track_code, role_id)
		)`,
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
		`create table if not exists user_notification_settings (
			user_id bigint primary key references users(id) on delete cascade,
			email_enabled boolean not null default false,
			updated_at timestamptz not null default now()
		)`,
		`create table if not exists user_follows (
			follower_id bigint not null references users(id) on delete cascade,
			followed_id bigint not null references users(id) on delete cascade,
			created_at timestamptz not null default now(),
			primary key (follower_id, followed_id),
			check (follower_id <> followed_id)
		)`,
		`create index if not exists idx_user_follows_followed_created_at on user_follows (followed_id, created_at desc)`,
		`create table if not exists notifications (
			id bigserial primary key,
			recipient_id bigint references users(id) on delete cascade,
			kind text not null,
			title text not null default '',
			body text not null default '',
			source_locale text not null default 'zh-CN',
			data jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`create index if not exists idx_notifications_recipient_updated_at on notifications (recipient_id, updated_at desc)`,
		`create index if not exists idx_notifications_broadcast_updated_at on notifications (updated_at desc) where recipient_id is null`,
		`create table if not exists notification_receipts (
			notification_id bigint not null references notifications(id) on delete cascade,
			user_id bigint not null references users(id) on delete cascade,
			read_at timestamptz,
			created_at timestamptz not null default now(),
			primary key (notification_id, user_id)
		)`,
		`create index if not exists idx_notification_receipts_user_read on notification_receipts (user_id, read_at)`,
		`create table if not exists notification_actors (
			notification_id bigint not null references notifications(id) on delete cascade,
			actor_id bigint not null references users(id) on delete cascade,
			created_at timestamptz not null default now(),
			primary key (notification_id, actor_id)
		)`,
		`create table if not exists notification_translations (
			notification_id bigint not null references notifications(id) on delete cascade,
			user_id bigint not null references users(id) on delete cascade,
			locale text not null,
			title text not null default '',
			body text not null default '',
			created_at timestamptz not null default now(),
			primary key (notification_id, user_id, locale)
		)`,
		`create table if not exists direct_conversations (
			id bigserial primary key,
			user_low_id bigint not null references users(id) on delete cascade,
			user_high_id bigint not null references users(id) on delete cascade,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			unique (user_low_id, user_high_id),
			check (user_low_id < user_high_id)
		)`,
		`create index if not exists idx_direct_conversations_updated_at on direct_conversations (updated_at desc)`,
		`create table if not exists direct_messages (
			id bigserial primary key,
			conversation_id bigint not null references direct_conversations(id) on delete cascade,
			sender_id bigint not null references users(id) on delete cascade,
			recipient_id bigint not null references users(id) on delete cascade,
			body text not null,
			read_at timestamptz,
			created_at timestamptz not null default now()
		)`,
		`create index if not exists idx_direct_messages_conversation_created_at on direct_messages (conversation_id, created_at desc)`,
		`create index if not exists idx_direct_messages_recipient_read on direct_messages (recipient_id, read_at, created_at desc)`,
		`create table if not exists user_chat_presence (
			user_id bigint primary key references users(id) on delete cascade,
			conversation_id bigint not null references direct_conversations(id) on delete cascade,
			expires_at timestamptz not null,
			updated_at timestamptz not null default now()
		)`,
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
		`alter table ai_tasks add column if not exists quota_reserved_tokens bigint not null default 0`,
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
		`create table if not exists mods (
			id bigserial primary key,
			project_code text unique,
			slug text not null unique,
			primary_name text not null,
			secondary_name text not null default '',
			abbreviation text not null default '',
			summary text not null default '',
			mod_id text not null default '',
			environment text not null default 'bothRequired',
			primary_category text not null default 'utility',
			official_status text not null default 'development',
			source_status text not null default 'unknown',
			license text not null default 'Custom',
			curseforge_project_id text not null default '',
			modrinth_project_id text not null default '',
			icon_url text not null default '',
			body_markdown text not null default '',
			search_keywords text[] not null default '{}'::text[],
			submission_method text not null default 'manual',
			review_status text not null default 'pending',
			created_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			published_at timestamptz,
			check (environment in ('clientOnly', 'serverOnly', 'bothRequired', 'clientOptional', 'serverOptional')),
			check (official_status in ('active', 'lowFrequency', 'discontinued', 'archived', 'development')),
			check (source_status in ('open', 'partial', 'closed', 'unknown')),
			check (submission_method in ('manual', 'modrinth', 'curseforge', 'github')),
			check (review_status in ('pending', 'approved', 'rejected'))
		)`,
		`alter table mods add column if not exists project_code text`,
		`update mods
		 set project_code = 'm' || substr(md5(id::text || slug || created_at::text), 1, 5) || (id % 10)::text
		 where project_code is null or project_code = ''`,
		`create unique index if not exists idx_mods_project_code on mods (project_code)`,
		`alter table mods alter column project_code set not null`,
		`do $$ begin
			if not exists (select 1 from pg_constraint where conname = 'mods_project_code_format') then
				alter table mods add constraint mods_project_code_format check (project_code ~ '^[a-z0-9]{7}$');
			end if;
		end $$`,
		`create or replace function prevent_mod_unique_id_update() returns trigger as $$
		begin
			if new.project_code is distinct from old.project_code then
				raise exception 'mod unique ID is immutable';
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`drop trigger if exists trg_mod_unique_id_immutable on mods`,
		`create trigger trg_mod_unique_id_immutable before update of project_code on mods
		 for each row execute function prevent_mod_unique_id_update()`,
		`create index if not exists idx_mods_review_updated_at on mods (review_status, updated_at desc)`,
		`create index if not exists idx_mods_created_by_updated_at on mods (created_by, updated_at desc)`,
		`create index if not exists idx_mods_primary_name_lower on mods (lower(primary_name))`,
		`create table if not exists mod_links (
			id bigserial primary key,
			mod_id bigint not null references mods(id) on delete cascade,
			link_type text not null,
			url text not null,
			display_order integer not null default 0,
			created_at timestamptz not null default now(),
			unique (mod_id, link_type, url)
		)`,
		`create index if not exists idx_mod_links_mod_order on mod_links (mod_id, display_order, id)`,
		`create table if not exists mod_tags (
			mod_id bigint not null references mods(id) on delete cascade,
			tag text not null,
			primary key (mod_id, tag)
		)`,
		`create table if not exists mod_authors (
			id bigserial primary key,
			mod_id bigint not null references mods(id) on delete cascade,
			name text not null,
			role text not null default '',
			display_order integer not null default 0,
			created_at timestamptz not null default now()
		)`,
		`create index if not exists idx_mod_authors_mod_order on mod_authors (mod_id, display_order, id)`,
		`create table if not exists mod_relationships (
			id bigserial primary key,
			mod_id bigint not null references mods(id) on delete cascade,
			relation_type text not null,
			related_mod_id bigint references mods(id) on delete set null,
			related_mod_name text not null default '',
			notes text not null default '',
			display_order integer not null default 0,
			created_at timestamptz not null default now(),
			check (relation_type in ('dependency', 'extension', 'integration')),
			check (related_mod_id is not null or related_mod_name <> '')
		)`,
		`create index if not exists idx_mod_relationships_mod_order on mod_relationships (mod_id, display_order, id)`,
		`create table if not exists mod_relationship_groups (
			id bigserial primary key,
			mod_id bigint not null references mods(id) on delete cascade,
			label text not null default '',
			loader text not null default '',
			minecraft_versions text[] not null default '{}'::text[],
			mod_version text not null default '',
			display_order integer not null default 0,
			created_at timestamptz not null default now()
		)`,
		`create index if not exists idx_mod_relationship_groups_mod_order on mod_relationship_groups (mod_id, display_order, id)`,
		`alter table mod_relationship_groups add column if not exists minecraft_versions text[] not null default '{}'::text[]`,
		`alter table mod_relationships add column if not exists group_id bigint references mod_relationship_groups(id) on delete cascade`,
		`create index if not exists idx_mod_relationships_group_order on mod_relationships (group_id, display_order, id)`,
		`create table if not exists mod_download_sources (
			id bigserial primary key,
			mod_id bigint not null references mods(id) on delete cascade,
			source_type text not null,
			label text not null default '',
			url text not null default '',
			display_order integer not null default 0,
			created_at timestamptz not null default now(),
			check (source_type in ('internal', 'modrinth', 'curseforge')),
			unique (mod_id, source_type, url)
		)`,
		`create index if not exists idx_mod_download_sources_mod_order on mod_download_sources (mod_id, display_order, id)`,
		`create table if not exists mod_loader_compatibilities (
			mod_id bigint not null references mods(id) on delete cascade,
			loader text not null,
			minecraft_version text not null,
			created_at timestamptz not null default now(),
			primary key (mod_id, loader, minecraft_version)
		)`,
		`create index if not exists idx_mod_loader_compatibilities_lookup on mod_loader_compatibilities (loader, minecraft_version, mod_id)`,
		`create table if not exists mod_membership_applications (
			id bigserial primary key,
			mod_id bigint not null references mods(id) on delete cascade,
			user_id bigint not null references users(id) on delete cascade,
			kind text not null,
			proof text not null,
			status text not null default 'pending',
			reviewed_by bigint references users(id) on delete set null,
			review_note text not null default '',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			reviewed_at timestamptz,
			check (kind in ('editor', 'developer')),
			check (status in ('pending', 'approved', 'rejected'))
		)`,
		`create unique index if not exists idx_mod_membership_applications_pending on mod_membership_applications (mod_id, user_id, kind) where status = 'pending'`,
		`create index if not exists idx_mod_membership_applications_review on mod_membership_applications (kind, status, created_at)`,
		`create table if not exists mod_application_attachments (
			application_id bigint not null references mod_membership_applications(id) on delete cascade,
			oss_file_id bigint not null references oss_files(id) on delete restrict,
			primary key (application_id, oss_file_id)
		)`,
		`create table if not exists mod_memberships (
			mod_id bigint not null references mods(id) on delete cascade,
			user_id bigint not null references users(id) on delete cascade,
			role text not null,
			granted_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			primary key (mod_id, user_id, role),
			check (role in ('editor', 'developer'))
		)`,
		`create table if not exists mod_comments (
			id bigserial primary key,
			mod_id bigint not null references mods(id) on delete cascade,
			user_id bigint not null references users(id) on delete cascade,
			parent_id bigint references mod_comments(id) on delete cascade,
			root_id bigint references mod_comments(id) on delete cascade,
			body text not null,
			status text not null default 'visible',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check (status in ('visible', 'hidden', 'deleted'))
		)`,
		`create index if not exists idx_mod_comments_mod_created on mod_comments (mod_id, created_at, id)`,
		`create index if not exists idx_mod_comments_root_created on mod_comments (root_id, created_at, id)`,
		`create table if not exists mod_comment_reactions (
			comment_id bigint not null references mod_comments(id) on delete cascade,
			user_id bigint not null references users(id) on delete cascade,
			reaction text not null,
			created_at timestamptz not null default now(),
			primary key (comment_id, user_id, reaction),
			check (reaction in ('thumbs_up', 'thumbs_down', 'laugh', 'hooray', 'confused', 'heart', 'rocket', 'eyes'))
		)`,
		`create table if not exists mod_revisions (
			id bigserial primary key,
			mod_id bigint not null references mods(id) on delete cascade,
			version integer not null,
			status text not null default 'pending',
			snapshot jsonb not null,
			change_reason text not null default '',
			submitted_by bigint references users(id) on delete set null,
			reviewed_by bigint references users(id) on delete set null,
			review_note text not null default '',
			created_at timestamptz not null default now(),
			reviewed_at timestamptz,
			check (status in ('pending', 'approved', 'rejected')),
			unique (mod_id, version)
		)`,
		`create index if not exists idx_mod_revisions_mod_version on mod_revisions (mod_id, version desc)`,
		`create index if not exists idx_mod_revisions_status_created on mod_revisions (status, created_at)`,
		`alter table mods add column if not exists current_revision_id bigint references mod_revisions(id) on delete set null`,
		`alter table mods add column if not exists supported_versions text[] not null default '{}'::text[]`,
		`alter table mods add column if not exists supported_loaders text[] not null default '{}'::text[]`,
		`insert into mod_revisions (mod_id, version, status, snapshot, change_reason, submitted_by, reviewed_at)
		 select m.id, 1, case when m.review_status in ('pending', 'approved', 'rejected') then m.review_status else 'pending' end,
			jsonb_build_object(
				'siteId', m.slug,
				'primaryName', m.primary_name, 'secondaryName', m.secondary_name, 'abbreviation', m.abbreviation,
				'summary', m.summary, 'modId', m.mod_id, 'environment', m.environment, 'primaryCategory', m.primary_category,
				'supportedVersions', to_jsonb(m.supported_versions), 'supportedLoaders', to_jsonb(m.supported_loaders),
				'tags', coalesce((select jsonb_agg(t.tag order by t.tag) from mod_tags t where t.mod_id = m.id), '[]'::jsonb),
				'searchKeywords', to_jsonb(m.search_keywords),
				'authors', coalesce((select jsonb_agg(jsonb_build_object('name', a.name, 'role', a.role) order by a.display_order, a.id) from mod_authors a where a.mod_id = m.id), '[]'::jsonb),
				'officialStatus', m.official_status, 'sourceStatus', m.source_status, 'license', m.license,
				'curseforgeProjectId', m.curseforge_project_id, 'modrinthProjectId', m.modrinth_project_id,
				'iconUrl', m.icon_url, 'bodyMarkdown', m.body_markdown, 'submissionMethod', m.submission_method,
				'links', coalesce((select jsonb_agg(jsonb_build_object('type', l.link_type, 'url', l.url) order by l.display_order, l.id) from mod_links l where l.mod_id = m.id), '[]'::jsonb),
				'relationshipGroups', coalesce((
					select jsonb_agg(jsonb_build_object(
						'label', g.label, 'loader', g.loader, 'minecraftVersions', to_jsonb(g.minecraft_versions), 'modVersion', g.mod_version,
						'relationships', coalesce((select jsonb_agg(jsonb_build_object('type', r.relation_type, 'relatedModId', r.related_mod_id, 'relatedModName', r.related_mod_name, 'notes', r.notes) order by r.display_order, r.id) from mod_relationships r where r.group_id = g.id), '[]'::jsonb)
					) order by g.display_order, g.id) from mod_relationship_groups g where g.mod_id = m.id
				), '[]'::jsonb)
			),
			'历史数据自动回填', m.created_by, case when m.review_status = 'pending' then null else coalesce(m.published_at, m.updated_at) end
		 from mods m where not exists (select 1 from mod_revisions r where r.mod_id = m.id)`,
		`update mod_revisions r
		 set snapshot = jsonb_set(r.snapshot, '{siteId}', to_jsonb(m.slug), true)
		 from mods m where m.id = r.mod_id and not (r.snapshot ? 'siteId')`,
		`update mods m set current_revision_id = r.id
		 from mod_revisions r where r.mod_id = m.id and r.version = 1 and m.current_revision_id is null and r.status = 'approved'`,
		`create table if not exists mod_export_packages (
			id text primary key,
			sha256 text not null unique check (sha256 ~ '^[0-9a-f]{64}$'),
			archive_file_id bigint references oss_files(id) on delete set null,
			archive_name text not null,
			schema_version text not null,
			exporter_version text not null,
			minecraft_version text not null,
			loader text not null,
			manifest jsonb not null,
			namespaces text[] not null default '{}'::text[],
			profile text not null default 'all',
			uploaded_by bigint references users(id) on delete set null,
			uploaded_at timestamptz not null default now(),
			imported_at timestamptz
		)`,
		`create table if not exists mod_export_jobs (
			id text primary key,
			mod_id bigint not null references mods(id) on delete cascade,
			package_id text not null references mod_export_packages(id) on delete cascade,
			importer_version text not null,
			status text not null default 'queued',
			progress smallint not null default 0 check (progress between 0 and 100),
			current_stage text not null default '',
			error_code text not null default '',
			error_detail jsonb not null default '{}'::jsonb,
			created_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			started_at timestamptz,
			finished_at timestamptz,
			updated_at timestamptz not null default now(),
			unique (mod_id, package_id, importer_version),
			check (status in ('queued','validating','importing','ready','partial','failed','cancelled'))
		)`,
		`create index if not exists idx_mod_export_jobs_status_created on mod_export_jobs(status, created_at)`,
		`alter table mod_export_jobs add column if not exists heartbeat_at timestamptz`,
		`alter table mod_export_jobs add column if not exists run_token text not null default ''`,
		`alter table mod_export_jobs add column if not exists attempt_count integer not null default 0`,
		`create index if not exists idx_mod_export_jobs_running_heartbeat
		 on mod_export_jobs(heartbeat_at) where status in ('validating','importing')`,
		`create table if not exists mod_export_revisions (
			id text primary key,
			mod_id bigint not null references mods(id) on delete cascade,
			package_id text not null references mod_export_packages(id) on delete restrict,
			revision_no bigint not null,
			status text not null default 'staging',
			minecraft_version text not null,
			loader text not null,
			exporter_version text not null,
			source_namespace text not null,
			source_metadata jsonb not null default '{}'::jsonb,
			is_active boolean not null default false,
			created_at timestamptz not null default now(),
			activated_at timestamptz,
			unique (mod_id, minecraft_version, loader, source_namespace, revision_no),
			check (status in ('staging','ready','partial','rejected','superseded'))
		)`,
		`create unique index if not exists idx_mod_export_revisions_active
		 on mod_export_revisions(mod_id, minecraft_version, loader, source_namespace) where is_active`,
		`create index if not exists idx_mod_export_revisions_package on mod_export_revisions(package_id)`,
		`alter table mod_export_revisions add column if not exists import_run_token text not null default ''`,
		`create index if not exists idx_mod_export_revisions_import_run on mod_export_revisions(import_run_token) where status='staging'`,
		`create table if not exists mod_export_locales (
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			locale text not null,
			translation_count integer not null default 0,
			primary key (revision_id, locale)
		)`,
		`create table if not exists mod_export_translations (
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			locale text not null,
			translation_key text not null,
			value text not null,
			primary key (revision_id, locale, translation_key)
		)`,
		`create index if not exists idx_mod_export_translations_key on mod_export_translations(revision_id, translation_key)`,
		`create table if not exists mod_export_registry_entries (
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			registry text not null,
			object_id text not null,
			namespace text not null,
			object_path text not null,
			translation_key text not null default '',
			names jsonb not null default '{}'::jsonb,
			data jsonb not null,
			primary key (revision_id, registry, object_id)
		)`,
		`create index if not exists idx_mod_export_registry_object on mod_export_registry_entries(object_id)`,
		`create table if not exists mod_export_text_assets (
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			asset_path text not null,
			asset_kind text not null,
			content_type text not null,
			sha256 text not null,
			byte_length bigint not null,
			text_content text,
			json_content jsonb,
			primary key (revision_id, asset_path),
			check ((text_content is not null)::integer + (json_content is not null)::integer = 1)
		)`,
		`create table if not exists mod_export_binary_assets (
			id text primary key,
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			asset_path text not null,
			asset_kind text not null,
			content_type text not null default 'application/octet-stream',
			sha256 text not null,
			byte_length bigint not null,
			data bytea not null,
			unique (revision_id, asset_path)
		)`,
		`create table if not exists mod_export_media (
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			asset_path text not null,
			media_kind text not null,
			oss_file_id bigint not null references oss_files(id) on delete restrict,
			sha256 text not null,
			content_type text not null,
			byte_length bigint not null,
			width integer,
			height integer,
			has_alpha boolean,
			primary key (revision_id, asset_path)
		)`,
		`create table if not exists mod_export_structures (
			id text primary key,
			revision_id text not null references mod_export_revisions(id) on delete cascade,
			structure_id text not null,
			asset_path text not null,
			source_format text not null,
			template_blob_id text not null references mod_export_binary_assets(id) on delete cascade,
			summary jsonb not null default '{}'::jsonb,
			unique (revision_id, structure_id)
		)`,
		`create table if not exists mod_export_entry_contents (
			mod_id bigint not null references mods(id) on delete cascade,
			registry text not null,
			object_id text not null,
			locale text not null,
			content_markdown text not null default '',
			updated_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			primary key (mod_id,registry,object_id,locale)
		)`,
		`create table if not exists mod_export_job_logs (
			id bigserial primary key,
			job_id text not null references mod_export_jobs(id) on delete cascade,
			level text not null default 'info',
			stage text not null default '',
			message text not null default '',
			created_at timestamptz not null default now()
		)`,
		`create table if not exists nats_outbox (
			id bigserial primary key,
			event_id text not null unique,
			subject text not null,
			aggregate_type text not null,
			aggregate_id text not null,
			payload jsonb not null,
			created_at timestamptz not null default now(),
			published_at timestamptz,
			attempts integer not null default 0,
			last_error text not null default ''
		)`,
		`create index if not exists idx_nats_outbox_pending on nats_outbox(created_at) where published_at is null`,
	}

	for _, statement := range statements {
		if _, err := db.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
