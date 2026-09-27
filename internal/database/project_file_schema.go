package database

func projectFileSchemaStatements() []string {
	return []string{
		`create table if not exists project_files (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check (public_id ~ '^[a-z0-9]{9}$'),
			project_type text not null,
			project_internal_id bigint not null,
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
			status text not null default 'processing',
			publication_generation integer not null default 0 check(publication_generation>=0),
			uploaded_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check (project_type in ('mod','modpack','plugin','map','resource_pack','shader_pack','datapack','addon')),
			check (release_channel in ('release','beta','alpha')),
			check (status in ('processing','active','rejected','deleted')),
			unique (project_type, project_internal_id, oss_file_id),
			foreign key(project_type,project_internal_id)
				references public_routes(entity_type,internal_id) on delete cascade
		)`,
		`create index if not exists idx_project_files_project_published
			on project_files(project_type,project_internal_id,status,created_at desc,id desc)`,
		`create index if not exists idx_project_files_filters
			on project_files(project_type,project_internal_id,release_channel) where status='active'`,
		`create or replace function ensure_project_file_publication_safe() returns trigger as $$
		begin
			if new.status='active' and not exists(
				select 1 from oss_files file where file.id=new.oss_file_id and file.status='active'
				  and file.scan_status in ('clean','trusted_generated')
			) then
				raise exception 'active project file requires a safe OSS object'
					using errcode='23514',constraint='project_files_active_oss_scan_check';
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_project_files_publication_safe before insert or update of status,oss_file_id on project_files
			for each row execute function ensure_project_file_publication_safe()`,
	}
}
