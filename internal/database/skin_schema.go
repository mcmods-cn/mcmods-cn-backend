package database

func skinSchemaStatements() []string {
	return []string{
		`create index idx_user_login_logs_yggdrasil_failures
			on user_login_logs(user_id,created_at desc)
			where success=false and reason like 'yggdrasil_%'`,
		`create index idx_user_login_logs_yggdrasil_ip_failures
			on user_login_logs(ip,created_at desc)
			where success=false and reason like 'yggdrasil_%'`,
		`create table yggdrasil_accounts (
			user_id bigint primary key references users(id) on delete cascade,
			account_uuid uuid not null unique,
			launcher_password_hash text not null default '',
			enabled boolean not null default false,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`create table skin_texture_blobs (
			hash text primary key check(hash ~ '^[0-9a-f]{64}$'),
			oss_file_id bigint not null unique references oss_files(id) on delete restrict,
			object_key text not null unique,
			width integer not null check(width > 0),
			height integer not null check(height > 0),
			size_bytes bigint not null check(size_bytes > 0),
			created_at timestamptz not null default now()
		)`,
		`create table skin_assets (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			owner_id bigint not null references users(id) on delete restrict,
			blob_hash text not null references skin_texture_blobs(hash) on delete restrict,
			kind text not null check(kind in ('skin','cape')),
			model text not null default 'default' check(model in ('default','slim')),
			display_name text not null,
			description text not null default '',
			tags text[] not null default '{}'::text[],
			visibility text not null default 'private' check(visibility in ('public','unlisted','private')),
			review_status text not null default 'approved' check(review_status in ('approved','pending','rejected')),
			status text not null default 'active' check(status in ('active','deleted')),
			downloads bigint not null default 0 check(downloads >= 0),
			published_revision_id bigint,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`create index idx_skin_assets_catalog on skin_assets(status,review_status,visibility,kind,created_at desc,id desc)`,
		`create index idx_skin_assets_owner on skin_assets(owner_id,status,updated_at desc,id desc)`,
		`create index idx_skin_assets_blob on skin_assets(blob_hash,id)`,
		`create index idx_skin_assets_tags on skin_assets using gin(tags)`,
		`create table skin_wardrobe (
			user_id bigint not null references users(id) on delete cascade,
			asset_id bigint not null references skin_assets(id) on delete cascade,
			added_at timestamptz not null default now(),
			primary key(user_id,asset_id)
		)`,
		`create index idx_skin_wardrobe_user_added on skin_wardrobe(user_id,added_at desc,asset_id)`,
		`create table skin_asset_adoptions (
			user_id bigint not null references users(id) on delete cascade,
			asset_id bigint not null references skin_assets(id) on delete cascade,
			created_at timestamptz not null default now(),
			primary key(user_id,asset_id)
		)`,
		`create table player_profiles (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			user_id bigint not null references users(id) on delete restrict,
			uuid uuid not null unique,
			name text not null check(name ~ '^[A-Za-z0-9_]{3,16}$'),
			bio text not null default '',
			visibility text not null default 'public' check(visibility in ('public','unlisted','private')),
			is_default boolean not null default false,
			status text not null default 'active' check(status in ('active','deleted')),
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`create unique index idx_player_profiles_name_lower on player_profiles(lower(name)) where status='active'`,
		`create unique index idx_player_profiles_default on player_profiles(user_id) where is_default and status='active'`,
		`create index idx_player_profiles_user on player_profiles(user_id,status,created_at,id)`,
		`create table player_profile_textures (
			profile_id bigint not null references player_profiles(id) on delete cascade,
			kind text not null check(kind in ('skin','cape')),
			asset_id bigint not null references skin_assets(id) on delete restrict,
			model text not null default 'default' check(model in ('default','slim')),
			equipped_at timestamptz not null default now(),
			primary key(profile_id,kind)
		)`,
		`create index idx_player_profile_textures_asset on player_profile_textures(asset_id,profile_id)`,
		`create table player_profile_name_history (
			id bigserial primary key,
			profile_id bigint not null references player_profiles(id) on delete cascade,
			old_name text not null,
			new_name text not null,
			changed_at timestamptz not null default now()
		)`,
		`create index idx_player_profile_name_history_profile on player_profile_name_history(profile_id,changed_at desc,id desc)`,
		`create table yggdrasil_tokens (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			access_token_hash text not null unique check(access_token_hash ~ '^[0-9a-f]{64}$'),
			user_id bigint not null references users(id) on delete cascade,
			player_profile_id bigint references player_profiles(id) on delete set null,
			client_token text not null check(char_length(client_token) between 1 and 512),
			status text not null default 'active' check(status in ('active','stale','revoked')),
			issued_at timestamptz not null default now(),
			expires_at timestamptz not null,
			last_used_at timestamptz,
			revoked_at timestamptz,
			replaced_by_id bigint references yggdrasil_tokens(id) on delete set null,
			ip text not null default '',
			user_agent text not null default ''
		)`,
		`create or replace function register_launcher_session_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id)
			values(new.public_id,'launcher_session',new.id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_yggdrasil_tokens_public_route after insert on yggdrasil_tokens
			for each row execute function register_launcher_session_public_route()`,
		`create or replace function remove_launcher_session_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type='launcher_session';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_yggdrasil_tokens_remove_public_route after delete on yggdrasil_tokens
			for each row execute function remove_launcher_session_public_route()`,
		`create index idx_yggdrasil_tokens_user_sessions on yggdrasil_tokens(user_id,issued_at desc,id desc)`,
		`create index idx_yggdrasil_tokens_active on yggdrasil_tokens(user_id,expires_at,id) where status in ('active','stale')`,
		`create index idx_yggdrasil_tokens_profile on yggdrasil_tokens(player_profile_id,status,expires_at) where player_profile_id is not null`,
		`create table yggdrasil_join_sessions (
			server_id text primary key check(char_length(server_id) between 1 and 255),
			token_id bigint not null references yggdrasil_tokens(id) on delete cascade,
			player_profile_id bigint not null references player_profiles(id) on delete cascade,
			client_ip text not null default '',
			expires_at timestamptz not null,
			created_at timestamptz not null default now()
		)`,
		`create index idx_yggdrasil_join_sessions_expires on yggdrasil_join_sessions(expires_at)`,

		`create or replace function register_skin_asset_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id,canonical_path)
			values(new.public_id,'skin',new.id,'/skins/'||new.public_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_skin_assets_public_route after insert on skin_assets
			for each row execute function register_skin_asset_public_route()`,
		`create or replace function remove_skin_asset_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type='skin';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_skin_assets_remove_public_route after delete on skin_assets
			for each row execute function remove_skin_asset_public_route()`,
		`create or replace function register_player_profile_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id,canonical_path)
			values(new.public_id,'player_profile',new.id,'/players/'||new.public_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_player_profiles_public_route after insert on player_profiles
			for each row execute function register_player_profile_public_route()`,
		`create or replace function remove_player_profile_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type='player_profile';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_player_profiles_remove_public_route after delete on player_profiles
			for each row execute function remove_player_profile_public_route()`,
	}
}
