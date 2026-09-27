package database

func baselineSchemaStatements() []string {
	statements := []string{
		`create table public_id_registry (
			public_id varchar(16) primary key check (public_id ~ '^[a-z0-9]{9}$'),
			created_at timestamptz not null default now()
		)`,
		`create or replace function new_public_id() returns text as $$
		declare
			alphabet constant text := 'abcdefghjkmnpqrstuvwxyz23456789';
			candidate text;
			entropy bytea;
			position integer;
			entropy_position integer;
			entropy_value integer;
		begin
			loop
				entropy := decode(replace(gen_random_uuid()::text, '-', ''), 'hex');
				candidate := '';
				position := 0;
				entropy_position := 0;
				while position < 9 loop
					if entropy_position >= length(entropy) then
						entropy := decode(replace(gen_random_uuid()::text, '-', ''), 'hex');
						entropy_position := 0;
					end if;
					entropy_value := get_byte(entropy, entropy_position);
					entropy_position := entropy_position + 1;
					if entropy_value < 248 then
						candidate := candidate || substr(alphabet, 1 + (entropy_value % length(alphabet)), 1);
						position := position + 1;
					end if;
				end loop;
				insert into public_id_registry(public_id) values(candidate)
				on conflict do nothing;
				exit when found;
			end loop;
			return candidate;
		end;
		$$ language plpgsql volatile`,
		`create table public_routes (
			id bigserial primary key,
			public_id varchar(16) not null unique check (public_id ~ '^[a-z0-9]{9}$'),
			entity_type text not null,
			internal_id bigint not null,
			canonical_path text not null default '',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			unique (entity_type, internal_id)
		)`,
		`create or replace function reserve_public_route_id() returns trigger as $$
		begin
			insert into public_id_registry(public_id) values(new.public_id)
			on conflict do nothing;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_public_routes_reserve_id before insert on public_routes
		 for each row execute function reserve_public_route_id()`,
		`create table if not exists users (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check (public_id ~ '^[a-z0-9]{9}$'),
			username text not null unique,
			email text not null unique,
			password_hash text not null,
			email_verified boolean not null default false,
			status text not null default 'active',
			country text not null default '',
			timezone text not null default 'Asia/Shanghai',
			preferred_content_language text not null default 'zh-CN',
			preferred_ui_language text not null default 'en-US',
			auth_version bigint not null default 1 check(auth_version > 0),
			permission_version bigint not null default 1 check(permission_version > 0),
			security_score integer not null default 100,
			registration_ip text not null default '',
			registration_country_code text not null default '',
			registration_city text not null default '',
			signature text not null default '',
			avatar_url text not null default '',
			avatar_file_id bigint,
			profile_revision_id bigint,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			last_login_at timestamptz
		)`,
		`create or replace function register_user_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id, entity_type, internal_id, canonical_path)
			values(new.public_id, 'user', new.id, '/user/' || new.public_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_users_public_route after insert on users
		 for each row execute function register_user_public_route()`,
		`create or replace function remove_user_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id = old.public_id and entity_type = 'user';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_users_remove_public_route after delete on users
		 for each row execute function remove_user_public_route()`,
		`create unique index uq_users_username_ci on users(lower(username))`,
		`create unique index uq_users_email_ci on users(lower(email))`,
		`create index idx_users_registration_ip_created
			on users(registration_ip,created_at desc) where registration_ip<>''`,
		`create table if not exists roles (
			id bigserial primary key,
			code text not null unique,
			name text not null,
			description text not null default '',
			weight integer not null default 0,
			parents text[] not null default '{}'::text[],
			status text not null default 'active',
			translations jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`create table if not exists permissions (
			id bigserial primary key,
			code text not null unique,
			module text not null,
			name text not null,
			description text not null default '',
			access_type text not null default 'read' check(access_type in ('read','write','moderation','administration','security')),
			translations jsonb not null default '{}'::jsonb
		)`,
		`create table if not exists role_permissions (
			role_id bigint not null references roles(id) on delete cascade,
			permission_id bigint not null references permissions(id) on delete cascade,
			allow boolean not null default true,
			expires_at timestamptz,
			updated_at timestamptz not null default now(),
			primary key (role_id, permission_id)
		)`,
		`create table if not exists user_role_bindings (
			user_id bigint not null references users(id) on delete cascade,
			role_id bigint not null references roles(id) on delete cascade,
			source text not null default 'manual'
				check(source in ('manual','account_status','governance_ban','level_track')),
			source_key text not null default '',
			expires_at timestamptz,
			created_at timestamptz not null default now(),
			primary key (user_id, role_id, source, source_key),
			check((source='manual' and source_key='') or (source<>'manual' and source_key<>''))
		)`,
		`create table if not exists user_permissions (
			user_id bigint not null references users(id) on delete cascade,
			permission_id bigint not null references permissions(id) on delete cascade,
			allow boolean not null default true,
			source text not null default 'manual'
				check(source in ('manual','user_preference','system_seed')),
			source_key text not null default '',
			expires_at timestamptz,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			primary key (user_id, permission_id, source, source_key),
			check((source='manual' and source_key='') or (source<>'manual' and source_key<>''))
		)`,
		`create index if not exists idx_role_permissions_permission_role
			on role_permissions(permission_id,role_id)`,
		`create index if not exists idx_user_role_bindings_role_user
			on user_role_bindings(role_id,user_id)`,
		`create index if not exists idx_user_role_bindings_user_source
			on user_role_bindings(user_id,source,source_key)`,
		`create index if not exists idx_user_permissions_permission_user
			on user_permissions(permission_id,user_id)`,
		`create index if not exists idx_user_permissions_user_source
			on user_permissions(user_id,source,source_key)`,
		`create or replace function bump_direct_user_permission_version() returns trigger as $$
		begin
			update users set permission_version=permission_version+1,updated_at=now()
			where id=coalesce(new.user_id,old.user_id);
			if tg_op='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_user_role_bindings_permission_version
			after insert or update or delete on user_role_bindings
			for each row execute function bump_direct_user_permission_version()`,
		`create trigger trg_user_permissions_permission_version
			after insert or update or delete on user_permissions
			for each row execute function bump_direct_user_permission_version()`,
		`create table if not exists permission_audit_logs (
			id bigserial primary key,
			operator_id bigint references users(id) on delete set null,
			target_user_id bigint references users(id) on delete set null,
			action text not null,
			payload jsonb not null default '{}'::jsonb,
			search_document tsvector not null default ''::tsvector,
			created_at timestamptz not null default now()
		)`,
		`create or replace function populate_permission_audit_log_search_document() returns trigger as $$
		declare operator_document text; target_document text;
		begin
			if new.operator_id is not null then
				select concat_ws(' ',public_id,username,email) into operator_document from users where id=new.operator_id;
			end if;
			if new.target_user_id is not null then
				select concat_ws(' ',public_id,username,email) into target_document from users where id=new.target_user_id;
			end if;
			new.search_document=to_tsvector('simple',left(lower(concat_ws(' ',new.action,new.payload::text,operator_document,target_document)),16384));
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_permission_audit_logs_search_document
			before insert or update of operator_id,target_user_id,action,payload on permission_audit_logs
			for each row execute function populate_permission_audit_log_search_document()`,
		`create index if not exists idx_permission_audit_logs_search_document
			on permission_audit_logs using gin(search_document)`,
		`create index if not exists idx_permission_audit_logs_created_at on permission_audit_logs (created_at desc, id desc)`,
		`create table if not exists user_login_logs (
			id bigserial primary key,
			user_id bigint references users(id) on delete set null,
			account text not null,
			ip text not null default '',
			user_agent text not null default '',
			country_code text not null default '',
			city text not null default '',
			success boolean not null,
			reason text not null default '',
			search_document tsvector not null default ''::tsvector,
			created_at timestamptz not null default now()
		)`,
		`create or replace function populate_user_login_log_search_document() returns trigger as $$
		declare user_document text;
		begin
			if new.user_id is not null then
				select concat_ws(' ',public_id,username,email) into user_document from users where id=new.user_id;
			end if;
			new.search_document=to_tsvector('simple',left(lower(concat_ws(' ',new.account,new.ip,new.user_agent,new.country_code,new.city,new.reason,user_document)),16384));
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_user_login_logs_search_document
			before insert or update of user_id,account,ip,user_agent,country_code,city,reason on user_login_logs
			for each row execute function populate_user_login_log_search_document()`,
		`create index if not exists idx_user_login_logs_search_document
			on user_login_logs using gin(search_document)`,
		`create index if not exists idx_user_login_logs_created_at on user_login_logs (created_at desc, id desc)`,
		`create index if not exists idx_user_login_logs_failed_ip
			on user_login_logs(ip,created_at desc) where success=false`,
		`create index if not exists idx_user_login_logs_failed_account
			on user_login_logs(lower(account),created_at desc) where success=false`,
		`create table if not exists user_registration_attempts (
			id bigserial primary key,
			ip text not null,
			created_at timestamptz not null default now()
		)`,
		`create index if not exists idx_user_registration_attempts_ip
			on user_registration_attempts(ip,created_at desc)`,
		`create table if not exists email_verification_codes (
			id bigserial primary key,
			email text not null,
			purpose text not null,
			code_hash text not null,
			request_ip text not null default '',
			expires_at timestamptz not null,
			consumed_at timestamptz,
			created_at timestamptz not null default now()
		)`,
		`create index if not exists idx_email_verification_codes_lookup on email_verification_codes (email, purpose, expires_at desc)`,
		`create index if not exists idx_email_verification_codes_rate_email
			on email_verification_codes(email,purpose,created_at desc)`,
		`create index if not exists idx_email_verification_codes_rate_ip
			on email_verification_codes(request_ip,created_at desc) where request_ip<>''`,
		`create table if not exists auth_sessions (
			id bigserial primary key,
			session_hash bytea not null unique check(octet_length(session_hash)=32),
			user_id bigint not null references users(id) on delete cascade,
			auth_version bigint not null check(auth_version>0),
			expires_at timestamptz not null,
			revoked_at timestamptz,
			created_at timestamptz not null default now()
		)`,
		`create index if not exists idx_auth_sessions_user_active
			on auth_sessions(user_id,expires_at desc) where revoked_at is null`,
		`create index if not exists idx_auth_sessions_expiry
			on auth_sessions(expires_at)`,
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
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			bucket text not null default '',
			endpoint text not null default '',
			region text not null default '',
			object_key text not null unique,
			category text not null default '',
			source text not null default '',
			original_name text not null default '',
			source_original_name text not null default '',
			content_type text not null default '',
			size_bytes bigint not null default 0,
			source_size_bytes bigint not null default 0,
			sha256 text not null default '',
			uploader_id bigint references users(id) on delete set null,
			status text not null default 'active',
			scan_status text not null default 'pending',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check(status in ('pending','active','deleted','quarantined')),
			check(scan_status in ('pending','clean','rejected','trusted_generated'))
		)`,
		`create index if not exists idx_oss_files_category on oss_files (category, created_at desc)`,
		`create index if not exists idx_oss_files_uploader_created_at on oss_files (uploader_id, created_at desc)`,
		`create index if not exists idx_oss_files_object_key_prefix on oss_files (object_key text_pattern_ops)`,
		`create index if not exists idx_oss_files_sha256_size on oss_files (sha256, size_bytes) where sha256 <> ''`,
		`create index if not exists idx_oss_files_sha256_source_size on oss_files (sha256, source_size_bytes) where sha256 <> ''`,
		`create index if not exists idx_oss_files_scan_status on oss_files (scan_status, created_at desc) where status = 'active'`,
		`create or replace function register_oss_file_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id)
			values(new.public_id,'oss_file',new.id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_oss_files_public_route after insert on oss_files
			for each row execute function register_oss_file_public_route()`,
		`create table oss_user_quota_usage (
			user_id bigint primary key references users(id) on delete cascade,
			active_source_bytes bigint not null default 0,
			active_stored_bytes bigint not null default 0,
			reserved_source_bytes bigint not null default 0,
			reserved_stored_bytes bigint not null default 0,
			calibrated_at timestamptz,
			updated_at timestamptz not null default now(),
			check(active_source_bytes>=0 and active_stored_bytes>=0 and reserved_source_bytes>=0 and reserved_stored_bytes>=0)
		)`,
		`create table oss_user_daily_quota_usage (
			user_id bigint not null references users(id) on delete cascade,
			usage_date date not null,
			active_source_bytes bigint not null default 0,
			active_stored_bytes bigint not null default 0,
			reserved_source_bytes bigint not null default 0,
			reserved_stored_bytes bigint not null default 0,
			calibrated_at timestamptz,
			updated_at timestamptz not null default now(),
			primary key(user_id,usage_date),
			check(active_source_bytes>=0 and active_stored_bytes>=0 and reserved_source_bytes>=0 and reserved_stored_bytes>=0)
		)`,
		`create table oss_user_upload_quota_reservations (
			user_id bigint not null references users(id) on delete cascade,
			object_key text not null,
			usage_date date not null,
			source_bytes bigint not null,
			stored_bytes bigint not null,
			expires_at timestamptz not null,
			created_at timestamptz not null default now(),
			primary key(user_id,object_key),
			check(source_bytes>0 and stored_bytes>=0 and expires_at>created_at)
		)`,
		`create index idx_oss_user_upload_quota_reservations_expiry
			on oss_user_upload_quota_reservations(user_id,expires_at,object_key)`,
		`create or replace function adjust_oss_user_active_quota_usage(
			p_user_id bigint,p_usage_date date,p_source_delta bigint,p_stored_delta bigint
		) returns void as $$
		begin
			if p_user_id is null then return; end if;
			insert into oss_user_quota_usage(user_id) values(p_user_id) on conflict do nothing;
			update oss_user_quota_usage set
				active_source_bytes=active_source_bytes+p_source_delta,
				active_stored_bytes=active_stored_bytes+p_stored_delta,
				updated_at=now()
			where user_id=p_user_id
			  and active_source_bytes+p_source_delta>=0
			  and active_stored_bytes+p_stored_delta>=0;
			if not found then raise exception 'OSS_USER_QUOTA_ACTIVE_UNDERFLOW'; end if;
			insert into oss_user_daily_quota_usage(user_id,usage_date) values(p_user_id,p_usage_date) on conflict do nothing;
			update oss_user_daily_quota_usage set
				active_source_bytes=active_source_bytes+p_source_delta,
				active_stored_bytes=active_stored_bytes+p_stored_delta,
				updated_at=now()
			where user_id=p_user_id and usage_date=p_usage_date
			  and active_source_bytes+p_source_delta>=0
			  and active_stored_bytes+p_stored_delta>=0;
			if not found then raise exception 'OSS_USER_DAILY_QUOTA_ACTIVE_UNDERFLOW'; end if;
		end;
		$$ language plpgsql`,
		`create or replace function maintain_oss_file_quota_usage() returns trigger as $$
		declare old_source bigint; new_source bigint;
		begin
			if tg_op='UPDATE'
			   and old.uploader_id is not distinct from new.uploader_id
			   and old.status=new.status and old.size_bytes=new.size_bytes
			   and old.source_size_bytes=new.source_size_bytes and old.created_at=new.created_at then
				return new;
			end if;
			if tg_op in ('UPDATE','DELETE') and old.uploader_id is not null and old.status='active' then
				old_source := coalesce(nullif(old.source_size_bytes,0),old.size_bytes);
				perform adjust_oss_user_active_quota_usage(old.uploader_id,old.created_at::date,-old_source,-old.size_bytes);
			end if;
			if tg_op in ('INSERT','UPDATE') and new.uploader_id is not null and new.status='active' then
				new_source := coalesce(nullif(new.source_size_bytes,0),new.size_bytes);
				perform adjust_oss_user_active_quota_usage(new.uploader_id,new.created_at::date,new_source,new.size_bytes);
			end if;
			if tg_op='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_oss_files_quota_usage after insert or update or delete on oss_files
			for each row execute function maintain_oss_file_quota_usage()`,
		`create or replace function rebuild_oss_user_quota_usage(p_user_id bigint) returns void as $$
		begin
			insert into oss_user_quota_usage(user_id,active_source_bytes,active_stored_bytes,calibrated_at,updated_at)
			select p_user_id,
				coalesce(sum(coalesce(nullif(source_size_bytes,0),size_bytes)),0),
				coalesce(sum(size_bytes),0),now(),now()
			from oss_files where uploader_id=p_user_id and status='active'
			on conflict(user_id) do update set
				active_source_bytes=excluded.active_source_bytes,
				active_stored_bytes=excluded.active_stored_bytes,
				calibrated_at=excluded.calibrated_at,updated_at=excluded.updated_at;
		end;
		$$ language plpgsql`,
		`create or replace function rebuild_oss_user_daily_quota_usage(p_user_id bigint,p_usage_date date) returns void as $$
		begin
			insert into oss_user_daily_quota_usage(user_id,usage_date,active_source_bytes,active_stored_bytes,calibrated_at,updated_at)
			select p_user_id,p_usage_date,
				coalesce(sum(coalesce(nullif(source_size_bytes,0),size_bytes)),0),
				coalesce(sum(size_bytes),0),now(),now()
			from oss_files where uploader_id=p_user_id and status='active' and created_at::date=p_usage_date
			on conflict(user_id,usage_date) do update set
				active_source_bytes=excluded.active_source_bytes,
				active_stored_bytes=excluded.active_stored_bytes,
				calibrated_at=excluded.calibrated_at,updated_at=excluded.updated_at;
		end;
		$$ language plpgsql`,
		`create or replace function remove_oss_file_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type='oss_file';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_oss_files_remove_public_route after delete on oss_files
			for each row execute function remove_oss_file_public_route()`,
		`create table oss_object_deletion_outbox (
			id bigserial primary key,
			oss_file_id bigint references oss_files(id) on delete set null,
			bucket text not null,
			endpoint text not null,
			region text not null,
			use_cname boolean not null default false,
			object_key text not null,
			reason text not null default '',
			status text not null default 'pending',
			attempts integer not null default 0,
			max_attempts integer not null default 12,
			next_attempt_at timestamptz not null default now(),
			locked_at timestamptz,
			locked_by text not null default '',
			last_error text not null default '',
			failure_class text not null default '',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			deleted_at timestamptz,
			dead_at timestamptz,
			replay_count integer not null default 0,
			last_replayed_at timestamptz,
			last_replayed_by bigint references users(id) on delete set null,
			unique(bucket,endpoint,object_key),
			check(status in ('pending','processing','completed','dead')),
			check(max_attempts between 1 and 100),
			check(replay_count >= 0),
			check(failure_class in ('','configuration','authentication','authorization','invalid_target','rate_limit','remote_transient','network','timeout','unknown','worker_lost'))
		)`,
		`create index idx_oss_object_deletion_outbox_pending
			on oss_object_deletion_outbox(next_attempt_at,id) where status in ('pending','processing')`,
		`create index idx_oss_object_deletion_outbox_dead
			on oss_object_deletion_outbox(dead_at desc,id desc) where status='dead'`,
		`create index idx_oss_object_deletion_outbox_exhausted
			on oss_object_deletion_outbox(updated_at,id)
			where status in ('pending','processing') and attempts>=max_attempts`,
		`create table oss_multipart_sessions (
			id bigserial primary key,
			owner_id bigint references users(id) on delete set null,
			bucket text not null,
			endpoint text not null,
			region text not null,
			use_cname boolean not null default false,
			object_key text not null,
			upload_id text not null,
			original_name text not null,
			content_type text not null,
			size_bytes bigint not null,
			sha256 text not null,
			category text not null,
			source text not null,
			status text not null default 'active',
			attempts integer not null default 0,
			max_attempts integer not null default 12,
			next_attempt_at timestamptz not null default now(),
			locked_by text not null default '',
			lease_expires_at timestamptz,
			last_error text not null default '',
			failure_class text not null default '',
			expires_at timestamptz not null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			completed_at timestamptz,
			aborted_at timestamptz,
			dead_at timestamptz,
			replay_count integer not null default 0,
			last_replayed_at timestamptz,
			last_replayed_by bigint references users(id) on delete set null,
			unique(bucket,endpoint,object_key,upload_id),
			check(size_bytes >= 0),
			check(sha256 ~ '^[0-9a-f]{64}$'),
			check(status in ('active','completing','cleanup_pending','aborting','completed','aborted','dead')),
			check(max_attempts between 1 and 100),
			check(replay_count >= 0),
			check(failure_class in ('','configuration','authentication','authorization','invalid_target','rate_limit','remote_transient','network','timeout','unknown','worker_lost'))
		)`,
		`create index idx_oss_multipart_sessions_expired
			on oss_multipart_sessions(expires_at,id) where status='active' and attempts<max_attempts`,
		`create index idx_oss_multipart_sessions_cleanup
			on oss_multipart_sessions(next_attempt_at,id) where status='cleanup_pending' and attempts<max_attempts`,
		`create index idx_oss_multipart_sessions_lease
			on oss_multipart_sessions(lease_expires_at,id) where status in ('completing','aborting') and attempts<max_attempts`,
		`create index idx_oss_multipart_sessions_exhausted
			on oss_multipart_sessions(updated_at,id) where status in ('active','completing','cleanup_pending','aborting') and attempts>=max_attempts`,
		`create index idx_oss_multipart_sessions_dead
			on oss_multipart_sessions(dead_at desc,id desc) where status='dead'`,
		`create table blueprints (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check (public_id ~ '^[a-z0-9]{9}$'),
			owner_id bigint not null references users(id) on delete cascade,
			title text not null default '',
			description_markdown text not null default '',
			source_format text not null,
			status text not null default 'uploading',
			upload_expires_at timestamptz,
			original_file_id bigint references oss_files(id) on delete set null,
			cover_file_id bigint references oss_files(id) on delete set null,
			original_object_key text not null default '',
			cover_object_key text not null default '',
			normalized_file_id bigint references oss_files(id) on delete set null,
			normalized_object_key text not null default '',
			size_x integer not null default 0,
			size_y integer not null default 0,
			size_z integer not null default 0,
			block_count bigint not null default 0,
			palette_count integer not null default 0,
			entity_count integer not null default 0,
			data_version integer not null default 0,
			last_error text not null default '',
			review_status text not null default 'not_required',
			published_revision_id bigint,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check (status in ('uploading','queued','processing','ready','partial','failed','deleted')),
			check (review_status in ('not_required','pending','approved','rejected'))
		)`,
		`create index idx_blueprints_owner_created on blueprints(owner_id, created_at desc)`,
		`create index idx_blueprints_status_updated on blueprints(status, updated_at desc)`,
		`create index idx_blueprints_upload_expiry on blueprints(upload_expires_at,id)
			where status='uploading' and upload_expires_at is not null`,
		`create unique index idx_blueprints_owner_original_file on blueprints(owner_id, original_file_id)
			where original_file_id is not null and status <> 'deleted'`,
		`create table blueprint_variants (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			blueprint_id bigint not null references blueprints(id) on delete cascade,
			format text not null,
			file_id bigint references oss_files(id) on delete set null,
			object_key text not null,
			original boolean not null default false,
			recommended boolean not null default false,
			status text not null default 'ready',
			original_name text not null default '',
			content_type text not null default 'application/octet-stream',
			size_bytes bigint not null default 0,
			sha256 text not null default '',
			created_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			unique (blueprint_id, object_key),
			check (status in ('queued','processing','ready','failed'))
		)`,
		`create index idx_blueprint_variants_blueprint on blueprint_variants(blueprint_id, original desc, created_at)`,
		`create or replace function register_blueprint_variant_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id)
			values(new.public_id,'blueprint_variant',new.id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_blueprint_variants_public_route after insert on blueprint_variants
			for each row execute function register_blueprint_variant_public_route()`,
		`create or replace function remove_blueprint_variant_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type='blueprint_variant';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_blueprint_variants_remove_public_route after delete on blueprint_variants
			for each row execute function remove_blueprint_variant_public_route()`,
		`create table blueprint_materials (
			blueprint_id bigint not null references blueprints(id) on delete cascade,
			block_state text not null,
			block_id text not null,
			properties jsonb not null default '{}'::jsonb check (jsonb_typeof(properties) = 'object'),
			block_count bigint not null,
			primary key (blueprint_id, block_state)
		)`,
		`create index idx_blueprint_materials_count on blueprint_materials(blueprint_id, block_count desc)`,
		`create table blueprint_jobs (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			blueprint_id bigint not null references blueprints(id) on delete cascade,
			operation text not null,
			target_format text not null default '',
			status text not null default 'queued',
			progress integer not null default 0,
			attempts integer not null default 0,
			max_attempts integer not null default 3,
			last_error text not null default '',
			locked_by text not null default '',
			lease_expires_at timestamptz,
			created_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			started_at timestamptz,
			finished_at timestamptz,
			updated_at timestamptz not null default now(),
			check (operation in ('normalize','convert')),
			check (status in ('queued','processing','completed','failed')),
			check (max_attempts between 1 and 20),
			check (progress between 0 and 100)
		)`,
		`create or replace function register_blueprint_job_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id)
			values(new.public_id,'blueprint_job',new.id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_blueprint_jobs_public_route after insert on blueprint_jobs
			for each row execute function register_blueprint_job_public_route()`,
		`create or replace function remove_blueprint_job_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type='blueprint_job';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_blueprint_jobs_remove_public_route after delete on blueprint_jobs
			for each row execute function remove_blueprint_job_public_route()`,
		`create index idx_blueprint_jobs_queue on blueprint_jobs(status, created_at)`,
		`create index idx_blueprint_jobs_recovery on blueprint_jobs(lease_expires_at,id) where status='processing'`,
		`create index idx_blueprint_jobs_creator_active on blueprint_jobs(created_by,id)
			where status in ('queued','processing') and created_by is not null`,
		`create unique index idx_blueprint_jobs_active_operation on blueprint_jobs(blueprint_id,operation,target_format)
			where status in ('queued','processing')`,
		`create table blueprint_job_artifacts (
			id bigserial primary key,
			job_id bigint references blueprint_jobs(id) on delete set null,
			blueprint_id bigint references blueprints(id) on delete set null,
			attempt integer not null,
			role text not null,
			file_id bigint not null unique references oss_files(id) on delete restrict,
			bucket text not null,
			endpoint text not null,
			region text not null,
			use_cname boolean not null default false,
			object_key text not null unique,
			status text not null default 'pending',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			activated_at timestamptz,
			abandoned_at timestamptz,
			unique(job_id,attempt,role),
			check(attempt > 0),
			check(role in ('normalized','cover','conversion')),
			check(status in ('pending','active','abandoned'))
		)`,
		`create index idx_blueprint_job_artifacts_pending
			on blueprint_job_artifacts(id,job_id,attempt) where status='pending'`,
		`create index idx_blueprint_job_artifacts_active_orphan
			on blueprint_job_artifacts(id) where status='active' and blueprint_id is null`,
		`create table favorite_collections (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			user_id bigint not null references users(id) on delete cascade,
			name text not null,
			is_default boolean not null default false,
			is_public boolean not null default false,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			unique(user_id,name)
		)`,
		`create unique index idx_favorite_collections_default on favorite_collections(user_id) where is_default`,
		`create index idx_favorite_collections_public on favorite_collections(user_id,created_at,id) where is_public`,
		`create table favorite_collection_items (
			id bigserial primary key,
			collection_id bigint not null references favorite_collections(id) on delete cascade,
			entity_type text not null check(entity_type in ('mod','modpack','blueprint')),
			entity_id bigint not null,
			created_at timestamptz not null default now(),
			unique(collection_id,entity_type,entity_id),
			foreign key(entity_type,entity_id) references public_routes(entity_type,internal_id) on delete cascade
		)`,
		`create index idx_favorite_items_entity on favorite_collection_items(entity_type,entity_id)`,
		`create or replace function register_blueprint_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id, entity_type, internal_id, canonical_path)
			values(new.public_id, 'blueprint', new.id, '/blueprints/' || new.public_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_blueprints_public_route after insert on blueprints
		 for each row execute function register_blueprint_public_route()`,
		`create or replace function remove_blueprint_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id = old.public_id and entity_type = 'blueprint';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_blueprints_remove_public_route after delete on blueprints
		 for each row execute function remove_blueprint_public_route()`,
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
			revision bigint not null default 1,
			save_session_id text not null default '',
			client_sequence bigint not null default 0,
			updated_at timestamptz not null default now(),
			check(revision > 0),
			check(client_sequence >= 0),
			check(length(save_session_id) <= 128)
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
			search_document tsvector not null default ''::tsvector,
			created_at timestamptz not null default now()
		)`,
		`create or replace function populate_oss_upload_log_search_document() returns trigger as $$
		declare uploader_document text;
		begin
			if new.uploader_id is not null then
				select concat_ws(' ',public_id,username,email) into uploader_document from users where id=new.uploader_id;
			end if;
			new.search_document=to_tsvector('simple',left(lower(concat_ws(' ',new.object_key,new.original_name,new.ip,new.user_agent,new.result,new.message,uploader_document)),16384));
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_oss_upload_logs_search_document
			before insert or update of uploader_id,object_key,original_name,ip,user_agent,result,message on oss_upload_logs
			for each row execute function populate_oss_upload_log_search_document()`,
		`create index if not exists idx_oss_upload_logs_search_document
			on oss_upload_logs using gin(search_document)`,
		`create index if not exists idx_oss_upload_logs_created_at on oss_upload_logs (created_at desc, id desc)`,
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
		`create index if not exists idx_oss_scan_logs_created_at on oss_scan_logs (created_at desc, id desc)`,
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
			search_document tsvector not null default ''::tsvector,
			created_at timestamptz not null default now()
		)`,
		`create or replace function populate_app_log_search_document() returns trigger as $$
		declare actor_document text;
		begin
			if new.actor_id is not null then
				select concat_ws(' ',public_id,username,email) into actor_document from users where id=new.actor_id;
			end if;
			new.search_document=to_tsvector('simple',left(lower(concat_ws(' ',new.action,new.target,new.ip,new.user_agent,new.method,new.path,new.status::text,new.payload::text,actor_document)),16384));
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_app_logs_search_document
			before insert or update of actor_id,action,target,ip,user_agent,method,path,status,payload on app_logs
			for each row execute function populate_app_log_search_document()`,
		`create index if not exists idx_app_logs_search_document on app_logs using gin(search_document)`,
		`create index if not exists idx_app_logs_category_created_at on app_logs (category, created_at desc, id desc)`,
		`create table if not exists user_notification_settings (
			user_id bigint primary key references users(id) on delete cascade,
			email_enabled boolean not null default false,
			project_updates_enabled boolean not null default true,
			updated_at timestamptz not null default now()
		)`,
		`create table if not exists user_follows (
			follower_id bigint not null references users(id) on delete cascade,
			followed_id bigint not null references users(id) on delete cascade,
			created_at timestamptz not null default now(),
			primary key (follower_id, followed_id),
			check (follower_id <> followed_id)
		)`,
		`create index if not exists idx_user_follows_followed_page on user_follows (followed_id, created_at desc, follower_id desc)`,
		`create index if not exists idx_user_follows_follower_page on user_follows (follower_id, created_at desc, followed_id desc)`,
		`create table if not exists notifications (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			recipient_id bigint references users(id) on delete cascade,
			kind text not null,
			title text not null default '',
			body text not null default '',
			source_locale text not null default 'zh-CN',
			template_key text,
			template_version integer not null default 0,
			template_params jsonb not null default '{}'::jsonb,
			data jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`create index if not exists idx_notifications_recipient_page on notifications (recipient_id, updated_at desc, id desc)`,
		`create index if not exists idx_notifications_recipient_kind_page on notifications (recipient_id, kind, updated_at desc, id desc)`,
		`create index if not exists idx_notifications_recipient_id on notifications (recipient_id, id)`,
		`create index if not exists idx_notifications_broadcast_page on notifications (updated_at desc, id desc) where recipient_id is null`,
		`create index if not exists idx_notifications_broadcast_kind_page on notifications (kind, updated_at desc, id desc) where recipient_id is null`,
		`create index if not exists idx_notifications_broadcast_id on notifications (id) where recipient_id is null`,
		`create table if not exists notification_broadcast_state (
			singleton boolean primary key default true check(singleton),
			live_count bigint not null default 0 check(live_count >= 0),
			max_notification_id bigint not null default 0 check(max_notification_id >= 0),
			updated_at timestamptz not null default now()
		)`,
		`insert into notification_broadcast_state(singleton) values(true) on conflict do nothing`,
		`create or replace function sync_notification_broadcast_state() returns trigger as $$
		declare
			delta bigint := 0;
			notification_id bigint := coalesce(new.id,old.id);
		begin
			if tg_op='INSERT' and new.recipient_id is null then
				delta := 1;
			elsif tg_op='DELETE' and old.recipient_id is null then
				delta := -1;
			elsif tg_op='UPDATE' then
				if old.recipient_id is null and new.recipient_id is not null then delta := -1; end if;
				if old.recipient_id is not null and new.recipient_id is null then delta := 1; end if;
			end if;
			if delta <> 0 then
				update notification_broadcast_state set live_count=live_count+delta,
					max_notification_id=greatest(max_notification_id,notification_id),updated_at=clock_timestamp()
				where singleton;
			end if;
			if tg_op='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_notifications_broadcast_state after insert or delete or update of recipient_id on notifications
			for each row execute function sync_notification_broadcast_state()`,
		`create or replace function register_notification_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id,canonical_path)
			values(new.public_id,'notification',new.id,'/messages');
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_notifications_public_route after insert on notifications
			for each row execute function register_notification_public_route()`,
		`create or replace function remove_notification_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type='notification';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_notifications_remove_public_route after delete on notifications
			for each row execute function remove_notification_public_route()`,
		`create table if not exists notification_receipts (
			notification_id bigint not null references notifications(id) on delete cascade,
			user_id bigint not null references users(id) on delete cascade,
			read_at timestamptz,
			created_at timestamptz not null default now(),
			primary key (notification_id, user_id)
		)`,
		`create index if not exists idx_notification_receipts_user_read on notification_receipts (user_id, read_at)`,
		`create table if not exists notification_read_watermarks (
			user_id bigint primary key references users(id) on delete cascade,
			max_notification_id bigint not null default 0 check(max_notification_id >= 0),
			read_at timestamptz not null,
			updated_at timestamptz not null default now()
		)`,
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
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			user_low_id bigint not null references users(id) on delete cascade,
			user_high_id bigint not null references users(id) on delete cascade,
			last_message_id bigint,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			unique (user_low_id, user_high_id),
			check (user_low_id < user_high_id)
		)`,
		`create index if not exists idx_direct_conversations_low_page on direct_conversations (user_low_id, updated_at desc, id desc)`,
		`create index if not exists idx_direct_conversations_high_page on direct_conversations (user_high_id, updated_at desc, id desc)`,
		`create table if not exists direct_messages (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			conversation_id bigint not null references direct_conversations(id) on delete cascade,
			sender_id bigint not null references users(id) on delete cascade,
			recipient_id bigint not null references users(id) on delete cascade,
			body text not null,
			read_at timestamptz,
			created_at timestamptz not null default now()
		)`,
		`alter table direct_conversations add constraint direct_conversations_last_message_id_fkey
			foreign key(last_message_id) references direct_messages(id) on delete set null`,
		`create index if not exists idx_direct_messages_conversation_id on direct_messages (conversation_id, id desc)`,
		`create index if not exists idx_direct_messages_recipient_read on direct_messages (recipient_id, read_at, created_at desc)`,
		`create index if not exists idx_direct_messages_unread_conversation on direct_messages (recipient_id, conversation_id, id) where read_at is null`,
		`create table if not exists direct_conversation_unread_counts (
			conversation_id bigint not null references direct_conversations(id) on delete cascade,
			user_id bigint not null references users(id) on delete cascade,
			unread_count bigint not null default 0 check(unread_count >= 0),
			updated_at timestamptz not null default now(),
			primary key(conversation_id,user_id)
		)`,
		`create or replace function sync_direct_conversation_unread_count() returns trigger as $$
		begin
			if tg_op = 'DELETE' then
				if old.read_at is null then
					update direct_conversation_unread_counts set unread_count=greatest(0,unread_count-1),updated_at=clock_timestamp()
					where conversation_id=old.conversation_id and user_id=old.recipient_id;
					delete from direct_conversation_unread_counts where conversation_id=old.conversation_id and user_id=old.recipient_id and unread_count=0;
				end if;
				return old;
			elsif tg_op = 'INSERT' then
				if new.read_at is null then
					insert into direct_conversation_unread_counts(conversation_id,user_id,unread_count)
					values(new.conversation_id,new.recipient_id,1)
					on conflict(conversation_id,user_id) do update
					set unread_count=direct_conversation_unread_counts.unread_count+1,updated_at=clock_timestamp();
				end if;
				return new;
			end if;
			if old.read_at is null and (new.read_at is not null or old.conversation_id<>new.conversation_id or old.recipient_id<>new.recipient_id) then
				update direct_conversation_unread_counts set unread_count=greatest(0,unread_count-1),updated_at=clock_timestamp()
				where conversation_id=old.conversation_id and user_id=old.recipient_id;
				delete from direct_conversation_unread_counts where conversation_id=old.conversation_id and user_id=old.recipient_id and unread_count=0;
			end if;
			if new.read_at is null and (old.read_at is not null or old.conversation_id<>new.conversation_id or old.recipient_id<>new.recipient_id) then
				insert into direct_conversation_unread_counts(conversation_id,user_id,unread_count)
				values(new.conversation_id,new.recipient_id,1)
				on conflict(conversation_id,user_id) do update
				set unread_count=direct_conversation_unread_counts.unread_count+1,updated_at=clock_timestamp();
			end if;
			return new;
		end $$ language plpgsql`,
		`create trigger trg_direct_messages_unread_count
			after insert or delete or update of conversation_id,recipient_id,read_at on direct_messages
			for each row execute function sync_direct_conversation_unread_count()`,
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
			quota_reserved_tokens bigint not null default 0,
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
		`create index if not exists idx_ai_tasks_recovery on ai_tasks(status,updated_at,id)
			where status in ('queued','retrying','running')`,
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
			project_code text not null unique check (project_code ~ '^[a-z0-9]{9}$'),
			slug text not null unique,
			primary_name text not null,
			secondary_name text not null default '',
			abbreviation text not null default '',
			summary text not null default '',
			environment text not null default 'bothRequired',
			primary_category text not null default 'utility',
			official_status text not null default 'development',
			source_status text not null default 'unknown',
			license text not null default 'Custom',
			curseforge_project_id text not null default '',
			modrinth_project_id text not null default '',
			github_project_path text not null default '',
			icon_url text not null default '',
			body_markdown text not null default '',
			search_keywords text[] not null default '{}'::text[],
			submission_method text not null default 'manual',
			review_status text not null default 'pending',
			submitted_by bigint references users(id) on delete set null,
			published_revision_id bigint,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			published_at timestamptz,
			check (environment in ('clientOnly', 'serverOnly', 'bothRequired', 'clientOptional', 'serverOptional')),
			check (official_status in ('active', 'lowFrequency', 'discontinued', 'archived', 'development')),
			check (source_status in ('open', 'partial', 'closed', 'unknown')),
			check (submission_method in ('manual', 'modrinth', 'curseforge', 'github')),
			check (review_status in ('pending', 'approved', 'rejected'))
		)`,
		`create unique index if not exists idx_mods_project_code on mods (project_code)`,
		`create or replace function prevent_mod_unique_id_update() returns trigger as $$
		begin
			if new.project_code is distinct from old.project_code then
				raise exception 'mod unique ID is immutable';
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_mod_unique_id_immutable before update of project_code on mods
		 for each row execute function prevent_mod_unique_id_update()`,
		`create or replace function register_mod_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id, entity_type, internal_id, canonical_path)
			values(new.project_code, 'mod', new.id, '/mods/' || new.slug);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_mods_public_route after insert on mods
		 for each row execute function register_mod_public_route()`,
		`create or replace function update_mod_public_route() returns trigger as $$
		begin
			update public_routes set canonical_path = '/mods/' || new.slug, updated_at = now()
			where public_id = new.project_code and entity_type = 'mod';
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_mods_update_public_route after update of slug on mods
		 for each row execute function update_mod_public_route()`,
		`create or replace function remove_mod_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id = old.project_code and entity_type = 'mod';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_mods_remove_public_route after delete on mods
		 for each row execute function remove_mod_public_route()`,
		`create index if not exists idx_mods_review_updated_at on mods (review_status, updated_at desc)`,
		`create index if not exists idx_mods_submitted_by_updated_at on mods (submitted_by, updated_at desc)`,
		`create index if not exists idx_mods_primary_name_lower on mods (lower(primary_name))`,
		`create table if not exists mod_identifiers (
			id bigserial primary key,
			mod_id bigint not null references mods(id) on delete cascade,
			identifier text not null,
			is_primary boolean not null default false,
			minecraft_version_min text not null default '',
			minecraft_version_max text not null default '',
			minecraft_versions text[] not null default '{}'::text[],
			display_order integer not null default 0,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check (identifier ~ '^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$')
		)`,
		`create unique index if not exists idx_mod_identifiers_namespace on mod_identifiers(lower(identifier))`,
		`create unique index if not exists idx_mod_identifiers_primary on mod_identifiers(mod_id) where is_primary`,
		`create index if not exists idx_mod_identifiers_mod_order on mod_identifiers(mod_id,display_order,id)`,
		`create table if not exists mod_gallery_images (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			mod_id bigint not null references mods(id) on delete cascade,
			oss_file_id bigint not null references oss_files(id) on delete restrict,
			display_order integer not null default 0,
			created_by bigint references users(id) on delete set null,
			published_revision_id bigint,
			created_at timestamptz not null default now(),
			unique(mod_id,oss_file_id)
		)`,
		`create index if not exists idx_mod_gallery_images_mod_order on mod_gallery_images(mod_id,display_order,id)`,
		`create table oss_rehome_jobs (
			id bigserial primary key,
			mod_id bigint not null unique references mods(id) on delete cascade,
			status text not null default 'queued',
			generation bigint not null default 1,
			claimed_generation bigint,
			attempts integer not null default 0,
			max_attempts integer not null default 8,
			next_attempt_at timestamptz not null default now(),
			locked_by text not null default '',
			lease_expires_at timestamptz,
			last_error text not null default '',
			failure_class text not null default '',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			completed_at timestamptz,
			dead_at timestamptz,
			replay_count integer not null default 0,
			last_replayed_at timestamptz,
			last_replayed_by bigint references users(id) on delete set null,
			check(status in ('queued','processing','completed','dead')),
			check(generation > 0),
			check(claimed_generation is null or claimed_generation > 0),
			check(max_attempts between 1 and 100),
			check(replay_count >= 0),
			check(failure_class in ('','configuration','authentication','authorization','invalid_target','rate_limit','remote_transient','network','timeout','unknown','worker_lost'))
		)`,
		`create index idx_oss_rehome_jobs_ready
			on oss_rehome_jobs(next_attempt_at,id) where status='queued' and attempts<max_attempts`,
		`create index idx_oss_rehome_jobs_recovery
			on oss_rehome_jobs(lease_expires_at,id) where status='processing' and attempts<max_attempts`,
		`create index idx_oss_rehome_jobs_exhausted
			on oss_rehome_jobs(updated_at,id) where status in ('queued','processing') and attempts>=max_attempts`,
		`create index idx_oss_rehome_jobs_dead
			on oss_rehome_jobs(dead_at desc,id desc) where status='dead'`,
		`create table if not exists mod_links (
			id bigserial primary key,
			mod_id bigint not null references mods(id) on delete cascade,
			link_type text not null,
			url text not null,
			note text not null default '',
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
		`create table if not exists content_creator_bindings (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			subject_type text not null,
			subject_id bigint not null,
			creator_id bigint not null,
			role_id bigint,
			name_snapshot text not null default '',
			role_snapshot text not null default '',
			status text not null default 'pending'
				check(status in ('pending','approved','rejected','revoked')),
			permission_granting boolean not null default false,
			approved_by bigint references users(id) on delete set null,
			approved_at timestamptz,
			display_order integer not null default 0,
			created_at timestamptz not null default now(),
			foreign key(subject_type,subject_id) references public_routes(entity_type,internal_id) on delete cascade
		)`,
		`create unique index if not exists idx_content_creator_bindings_unique
			on content_creator_bindings(subject_type,subject_id,creator_id,coalesce(role_id,0))`,
		`create index if not exists idx_content_creator_bindings_subject_order
			on content_creator_bindings(subject_type,subject_id,display_order,id)`,
		`create index if not exists idx_content_creator_bindings_review_page
			on content_creator_bindings(status,created_at,id)`,
		`create table if not exists mod_relationships (
			id bigserial primary key,
			mod_id bigint not null references mods(id) on delete cascade,
			relation_type text not null,
			related_mod_id bigint references mods(id) on delete set null,
			group_id bigint,
			related_mod_name text not null default '',
			related_mod_identifier text not null default '',
			display_order integer not null default 0,
			created_at timestamptz not null default now(),
			check (relation_type in ('dependency', 'integration', 'conflict')),
			check (related_mod_id is not null or related_mod_name <> '' or related_mod_identifier <> '')
		)`,
		`create index if not exists idx_mod_relationships_mod_order on mod_relationships (mod_id, display_order, id)`,
		`create index if not exists idx_mod_relationships_related_mod on mod_relationships (related_mod_id, id) where related_mod_id is not null`,
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
		`create index if not exists idx_mod_relationships_group_order on mod_relationships (group_id, display_order, id)`,
		`create table if not exists unresolved_references (
			id bigserial primary key,
			source_type text not null,
			source_id bigint not null,
			field_path text not null default '',
			reference_type text not null,
			raw_identifier text not null,
			normalized_identifier text not null,
			resolved_type text not null default '',
			resolved_id bigint,
			status text not null default 'pending',
			metadata jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			resolved_at timestamptz,
			unique(source_type,source_id,field_path,reference_type,normalized_identifier),
			check(status in ('pending','resolved','ignored')),
			check(raw_identifier <> '' and normalized_identifier <> '')
		)`,
		`create index if not exists idx_unresolved_references_pending
			on unresolved_references(reference_type,normalized_identifier,id) where status='pending'`,
		`create index if not exists idx_unresolved_references_source
			on unresolved_references(source_type,source_id,id)`,
		`create or replace function remove_mod_relationship_unresolved_references() returns trigger as $$
		begin
			delete from unresolved_references
			where source_type='mod_relationship' and source_id=old.id;
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_mod_relationships_remove_unresolved after delete on mod_relationships
		 for each row execute function remove_mod_relationship_unresolved_references()`,
		`create table if not exists mod_loader_compatibilities (
			mod_id bigint not null references mods(id) on delete cascade,
			loader text not null,
			minecraft_version text not null,
			created_at timestamptz not null default now(),
			primary key (mod_id, loader, minecraft_version)
		)`,
		`create index if not exists idx_mod_loader_compatibilities_lookup on mod_loader_compatibilities (loader, minecraft_version, mod_id)`,
		`create table if not exists project_editor_applications (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			target_route_id bigint not null references public_routes(id) on delete cascade,
			user_id bigint not null references users(id) on delete cascade,
			proof_markdown text not null,
			status text not null default 'pending',
			reviewed_by bigint references users(id) on delete set null,
			review_note text not null default '',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			reviewed_at timestamptz,
			check (status in ('pending', 'approved', 'rejected', 'withdrawn'))
		)`,
		`create unique index if not exists idx_project_editor_applications_pending
			on project_editor_applications(target_route_id,user_id) where status='pending'`,
		`create index if not exists idx_project_editor_applications_review
			on project_editor_applications(status,created_at,id)`,
		`create or replace function register_project_editor_application_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id)
			values(new.public_id,'project_editor_application',new.id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_project_editor_applications_public_route after insert on project_editor_applications
			for each row execute function register_project_editor_application_public_route()`,
		`create or replace function remove_project_editor_application_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type='project_editor_application';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_project_editor_applications_remove_public_route after delete on project_editor_applications
			for each row execute function remove_project_editor_application_public_route()`,
		`create table if not exists project_editor_application_attachments (
			application_id bigint not null references project_editor_applications(id) on delete cascade,
			oss_file_id bigint not null references oss_files(id) on delete restrict,
			primary key (application_id, oss_file_id)
		)`,
		`create table if not exists project_editor_assignments (
			target_route_id bigint not null references public_routes(id) on delete cascade,
			user_id bigint not null references users(id) on delete cascade,
			application_id bigint references project_editor_applications(id) on delete set null,
			granted_by bigint references users(id) on delete set null,
			status text not null default 'active' check(status in ('active','revoked')),
			revoked_by bigint references users(id) on delete set null,
			revoked_at timestamptz,
			revoke_reason text not null default '',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			primary key (target_route_id, user_id)
		)`,
		`create table if not exists catalog_import_packages (
			id text primary key,
			sha256 text not null check (sha256 ~ '^[0-9a-f]{64}$'),
			archive_file_id bigint not null unique references oss_files(id) on delete restrict,
			archive_name text not null,
			schema_version text not null,
			exporter_version text not null,
			minecraft_version text not null,
			loader text not null,
			manifest jsonb not null,
			namespaces text[] not null default '{}'::text[],
			profile text not null default 'all',
			uploaded_by bigint not null references users(id) on delete restrict,
			uploaded_at timestamptz not null default now(),
			content_verified_at timestamptz,
			imported_at timestamptz
		)`,
		`create index if not exists idx_catalog_import_packages_verified_sha
			on catalog_import_packages(sha256,content_verified_at desc) where content_verified_at is not null`,
		`create or replace function prevent_catalog_import_package_source_mutation() returns trigger as $$
		begin
			if new.sha256 is distinct from old.sha256
				or new.archive_file_id is distinct from old.archive_file_id
				or new.archive_name is distinct from old.archive_name
				or new.uploaded_by is distinct from old.uploaded_by then
				raise exception 'catalog import package source is immutable';
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_catalog_import_package_source_immutable
			before update on catalog_import_packages for each row
			execute function prevent_catalog_import_package_source_mutation()`,
		`create table if not exists catalog_import_jobs (
			id text primary key,
			mod_id bigint not null references mods(id) on delete cascade,
			package_id text not null references catalog_import_packages(id) on delete cascade,
			target_version_id bigint not null,
			overwrite_existing boolean not null default false,
			importer_version text not null,
			status text not null default 'queued',
			progress smallint not null default 0 check (progress between 0 and 100),
			current_stage text not null default '',
			error_code text not null default '',
			error_detail jsonb not null default '{}'::jsonb,
			created_by bigint references users(id) on delete set null,
			detected_modids jsonb not null default '[]'::jsonb,
			configured_modids text[] not null default '{}'::text[],
			primary_detected_modid text not null default '',
			modid_analysis_hash text not null default '',
			modid_confirmation_required boolean not null default false,
			modid_confirmed_at timestamptz,
			modid_confirmed_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			started_at timestamptz,
			finished_at timestamptz,
			heartbeat_at timestamptz,
			run_token text not null default '',
			attempt_count integer not null default 0,
			updated_at timestamptz not null default now(),
			unique (mod_id, package_id, importer_version, target_version_id, overwrite_existing),
			check (status in ('queued','validating','confirmation_required','importing','ready','partial','failed','cancelled'))
		)`,
		`create index if not exists idx_catalog_import_jobs_status_created on catalog_import_jobs(status, created_at)`,
		`create index if not exists idx_catalog_import_jobs_running_heartbeat
		 on catalog_import_jobs(heartbeat_at) where status in ('validating','importing')`,
		`create table if not exists catalog_import_revisions (
			id text primary key,
			mod_id bigint not null references mods(id) on delete cascade,
			package_id text not null references catalog_import_packages(id) on delete restrict,
			job_id text not null references catalog_import_jobs(id) on delete cascade,
			target_version_id bigint not null,
			revision_no bigint not null,
			status text not null default 'staging',
			minecraft_version text not null,
			loader text not null,
			exporter_version text not null,
			source_namespace text not null,
			source_kind text not null default 'mcmods_exporter',
			source_metadata jsonb not null default '{}'::jsonb,
			import_run_token text not null default '',
			submitted_by bigint references users(id) on delete set null,
			submitted_by_snapshot text not null default '',
			is_active boolean not null default false,
			created_at timestamptz not null default now(),
			activated_at timestamptz,
			unique (mod_id, target_version_id, source_kind, source_namespace, revision_no),
			check (status in ('staging','ready','partial','rejected','superseded'))
		)`,
		`create or replace function attribute_catalog_import_revision() returns trigger as $$
		begin
			if new.submitted_by is null then
				select coalesce(job.created_by,package.uploaded_by),
					coalesce(account.username,'system')
			into new.submitted_by,new.submitted_by_snapshot
			from catalog_import_jobs job
			join catalog_import_packages package on package.id=job.package_id
			left join users account on account.id=coalesce(job.created_by,package.uploaded_by)
			where job.id=new.job_id;
			end if;
			if new.submitted_by_snapshot='' then
				select coalesce(account.username,'system') into new.submitted_by_snapshot
				from (values(1)) seed(value)
				left join users account on account.id=new.submitted_by;
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_catalog_import_revision_submitter before insert on catalog_import_revisions
			for each row execute function attribute_catalog_import_revision()`,
		`create unique index if not exists idx_catalog_import_revisions_active
		 on catalog_import_revisions(mod_id, target_version_id, source_kind, source_namespace) where is_active`,
		`create index idx_catalog_import_revisions_material_namespace
		 on catalog_import_revisions(source_namespace,coalesce(activated_at,created_at) desc,id desc)
		 where is_active and status in ('ready','partial')`,
		`create index if not exists idx_catalog_import_revisions_package on catalog_import_revisions(package_id)`,
		`create index if not exists idx_catalog_import_revisions_import_run on catalog_import_revisions(import_run_token) where status='staging'`,
		`create table if not exists catalog_import_locales (
			revision_id text not null references catalog_import_revisions(id) on delete cascade,
			locale text not null,
			translation_count integer not null default 0,
			primary key (revision_id, locale)
		)`,
		`create table if not exists catalog_import_text_assets (
			revision_id text not null references catalog_import_revisions(id) on delete cascade,
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
		`create table if not exists catalog_import_binary_assets (
			id text primary key,
			revision_id text not null references catalog_import_revisions(id) on delete cascade,
			asset_path text not null,
			asset_kind text not null,
			content_type text not null default 'application/octet-stream',
			sha256 text not null,
			byte_length bigint not null,
			data bytea not null,
			unique (revision_id, asset_path)
		)`,
		`create table if not exists catalog_import_media (
			revision_id text not null references catalog_import_revisions(id) on delete cascade,
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
		`create table if not exists catalog_import_structures (
			id text primary key,
			revision_id text not null references catalog_import_revisions(id) on delete cascade,
			structure_id text not null,
			asset_path text not null,
			source_format text not null,
			template_blob_id text not null references catalog_import_binary_assets(id) on delete cascade,
			summary jsonb not null default '{}'::jsonb,
			unique (revision_id, structure_id)
		)`,
		`create table if not exists catalog_import_job_logs (
			id bigserial primary key,
			job_id text not null references catalog_import_jobs(id) on delete cascade,
			level text not null default 'info',
			stage text not null default '',
			message text not null default '',
			created_at timestamptz not null default now()
		)`,
		`create table if not exists nats_outbox (
			id bigserial primary key,
			event_id text not null unique,
			event_type text not null default '',
			schema_version integer not null default 1 check(schema_version > 0),
			subject text not null,
			aggregate_type text not null,
			aggregate_id text not null,
			trace_id text not null default '',
			payload jsonb not null,
			occurred_at timestamptz not null default now(),
			status text not null default 'pending',
			available_at timestamptz not null default now(),
			created_at timestamptz not null default now(),
			published_at timestamptz,
			locked_at timestamptz,
			locked_by text not null default '',
			attempts integer not null default 0,
			max_attempts integer not null default 12 check(max_attempts > 0),
			last_error text not null default '',
			updated_at timestamptz not null default now(),
			constraint nats_outbox_status_check check(status in ('pending','publishing','published','failed','dead'))
		)`,
		`create index if not exists idx_nats_outbox_pending on nats_outbox(available_at,id)
			where status in ('pending','failed') and published_at is null`,
		`create index if not exists idx_nats_outbox_status_created on nats_outbox(status,created_at)`,
		`create index if not exists idx_nats_outbox_aggregate on nats_outbox(aggregate_type,aggregate_id,id)`,
		`create table if not exists mod_metadata_import_jobs (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			user_id bigint not null references users(id) on delete cascade,
			provider text not null,
			source_url text not null,
			status text not null default 'queued',
			progress integer not null default 0,
			result jsonb,
			error text not null default '',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			started_at timestamptz,
			finished_at timestamptz,
			check (provider in ('modrinth','curseforge','github')),
			check (status in ('queued','running','completed','failed')),
			check (progress between 0 and 100)
		)`,
		`create or replace function register_mod_metadata_import_job_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id)
			values(new.public_id,'mod_metadata_import_job',new.id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_mod_metadata_import_jobs_public_route after insert on mod_metadata_import_jobs
			for each row execute function register_mod_metadata_import_job_public_route()`,
		`create or replace function remove_mod_metadata_import_job_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type='mod_metadata_import_job';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_mod_metadata_import_jobs_remove_public_route after delete on mod_metadata_import_jobs
			for each row execute function remove_mod_metadata_import_job_public_route()`,
		`create index if not exists idx_mod_metadata_import_jobs_user_created on mod_metadata_import_jobs(user_id, created_at desc)`,
		`create index if not exists idx_mod_metadata_import_jobs_queued on mod_metadata_import_jobs(created_at) where status='queued'`,
	}

	return statements
}
