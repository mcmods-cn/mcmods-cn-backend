package database

func projectFileSchemaStatements() []string {
	return []string{
		`create table if not exists project_files (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check (public_id ~ '^[a-z0-9]{9}$'),
			project_type text not null,
			project_id text not null check (project_id ~ '^[a-z0-9]{9}$'),
			oss_file_id bigint not null references oss_files(id) on delete restrict,
			display_name text not null default '',
			version_name text not null default '',
			release_channel text not null default 'release',
			game_versions text[] not null default '{}'::text[],
			loaders text[] not null default '{}'::text[],
			file_name text not null,
			content_type text not null default 'application/java-archive',
			size_bytes bigint not null default 0,
			sha256 text not null default '',
			download_count bigint not null default 0,
			status text not null default 'active',
			uploaded_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check (project_type in ('mod','modpack','plugin','map','resource_pack','shader_pack','datapack')),
			check (release_channel in ('release','beta','alpha')),
			check (status in ('active','deleted')),
			unique (project_type, project_id, oss_file_id)
		)`,
		`create index if not exists idx_project_files_project_published
			on project_files(project_type,project_id,status,created_at desc,id desc)`,
		`create index if not exists idx_project_files_filters
			on project_files(project_type,project_id,release_channel) where status='active'`,
		`create or replace function register_project_file_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,entity_key,canonical_path)
			values(new.public_id,'project_file',new.id::text,'/api/v1/project-files/' || new.public_id || '/download');
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_project_files_public_route after insert on project_files
			for each row execute function register_project_file_public_route()`,
		`create or replace function remove_project_file_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type='project_file';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_project_files_remove_public_route after delete on project_files
			for each row execute function remove_project_file_public_route()`,
	}
}
