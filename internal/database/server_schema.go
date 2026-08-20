package database

// serverSchemaStatements installs the public Minecraft server catalog, its
// review evidence, detected mod references, and five-minute status history as
// part of the current development schema generation. Older development
// databases are reset instead of migrated.
func serverSchemaStatements() []string {
	return []string{
		`create table minecraft_servers (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			slug text not null unique,
			address text not null,
			normalized_address text not null,
			handshake_host text not null,
			connect_host text not null,
			connect_port integer not null check(connect_port between 1 and 65535),
			name text not null check(char_length(name) between 1 and 80),
			short_description text not null default '' check(char_length(short_description) <= 240),
			body_markdown text not null default '' check(char_length(body_markdown) <= 100000),
			minecraft_versions text[] not null default '{}'::text[],
			dedicated_client boolean not null default false,
			languages text[] not null default '{}'::text[],
			primary_tag text not null,
			has_whitelist boolean not null default false,
			online_mode boolean not null default true,
			icon_data_uri text not null default '' check(char_length(icon_data_uri) <= 1048576),
			modded boolean not null default false,
			loader text not null default '',
			mod_list_complete boolean not null default false,
			review_status text not null default 'pending',
			review_note text not null default '',
			proof_text text not null default '' check(char_length(proof_text) <= 10000),
			submitted_by bigint not null references users(id) on delete restrict,
			reviewed_by bigint references users(id) on delete set null,
			last_online boolean not null default false,
			last_latency_ms integer check(last_latency_ms is null or last_latency_ms >= 0),
			last_players_online integer not null default 0 check(last_players_online >= 0),
			last_players_max integer not null default 0 check(last_players_max >= 0),
			last_motd text not null default '',
			last_minecraft_version text not null default '',
			last_protocol integer,
			last_checked_at timestamptz,
			last_error text not null default '',
			next_probe_at timestamptz not null default now(),
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			reviewed_at timestamptz,
			published_at timestamptz,
			check(primary_tag in ('survival','casual','adventure','creative','war','rpg','minigame','technology')),
			check(review_status in ('pending','approved','rejected'))
		)`,
		`create index idx_minecraft_servers_catalog
			on minecraft_servers(review_status,primary_tag,updated_at desc,id desc)`,
		`create index idx_minecraft_servers_probe
			on minecraft_servers(next_probe_at,id) where review_status='approved'`,
		`create unique index idx_minecraft_servers_active_address
			on minecraft_servers(normalized_address) where review_status in ('pending','approved')`,
		`create index idx_minecraft_servers_versions
			on minecraft_servers using gin(minecraft_versions)`,
		`create index idx_minecraft_servers_languages
			on minecraft_servers using gin(languages)`,
		`create index idx_minecraft_servers_submitted_by
			on minecraft_servers(submitted_by,updated_at desc,id desc)`,
		`create index idx_minecraft_servers_search
			on minecraft_servers using gin(to_tsvector('simple',name||' '||short_description||' '||body_markdown))`,

		`create table minecraft_server_links (
			id bigserial primary key,
			server_id bigint not null references minecraft_servers(id) on delete cascade,
			kind text not null default 'website' check(kind in ('website','forum','discord','qq','bilibili','other')),
			label text not null default '',
			url text not null check(char_length(url) between 1 and 2048),
			display_order integer not null default 0
		)`,
		`create index idx_minecraft_server_links_order
			on minecraft_server_links(server_id,display_order,id)`,

		`create table minecraft_server_mods (
			id bigserial primary key,
			server_id bigint not null references minecraft_servers(id) on delete cascade,
			mod_id bigint references mods(id) on delete set null,
			raw_mod_id text not null,
			version text not null default '',
			source text not null default 'manual' check(source in ('forge_status','configuration','agent','manual')),
			confidence text not null default 'declared' check(confidence in ('exact','high','inferred','declared')),
			created_at timestamptz not null default now(),
			unique(server_id,raw_mod_id)
		)`,
		`create index idx_minecraft_server_mods_mod
			on minecraft_server_mods(mod_id,server_id) where mod_id is not null`,
		`create index idx_minecraft_server_mods_raw
			on minecraft_server_mods(lower(raw_mod_id),server_id)`,

		`create table minecraft_server_proof_files (
			server_id bigint not null references minecraft_servers(id) on delete cascade,
			oss_file_id bigint not null references oss_files(id) on delete restrict,
			display_order integer not null default 0,
			created_at timestamptz not null default now(),
			primary key(server_id,oss_file_id)
		)`,

		`create table minecraft_server_status_samples (
			id bigserial primary key,
			server_id bigint not null references minecraft_servers(id) on delete cascade,
			checked_at timestamptz not null default now(),
			online boolean not null,
			latency_ms integer check(latency_ms is null or latency_ms >= 0),
			players_online integer check(players_online is null or players_online >= 0),
			players_max integer check(players_max is null or players_max >= 0),
			minecraft_version text not null default '',
			protocol integer,
			error text not null default ''
		)`,
		`create index idx_minecraft_server_samples_history
			on minecraft_server_status_samples(server_id,checked_at desc,id desc)`,
		`create index idx_minecraft_server_samples_retention
			on minecraft_server_status_samples(checked_at,id)`,

		`create or replace function register_minecraft_server_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id,canonical_path)
			values(new.public_id,'minecraft_server',new.id,'/servers/'||new.public_id)
			on conflict(public_id) do update
			set entity_type=excluded.entity_type,internal_id=excluded.internal_id,
				canonical_path=excluded.canonical_path,updated_at=now();
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_minecraft_servers_public_route after insert on minecraft_servers
			for each row execute function register_minecraft_server_public_route()`,
		`create or replace function remove_minecraft_server_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type='minecraft_server';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_minecraft_servers_remove_public_route after delete on minecraft_servers
			for each row execute function remove_minecraft_server_public_route()`,

		`create or replace function remove_minecraft_server_mod_unresolved_reference() returns trigger as $$
		begin
			delete from unresolved_references
			where source_type='minecraft_server_mod' and source_id=old.id;
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_minecraft_server_mods_remove_unresolved after delete on minecraft_server_mods
			for each row execute function remove_minecraft_server_mod_unresolved_reference()`,
	}
}
